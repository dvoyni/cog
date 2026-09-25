package internal

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(f *frame, op *Op, pass descriptors.PassDescr, firstErr *error) {
	m := &op.Mesh
	stride := descriptors.MeshStride(m)
	vertices, indices := descriptors.MeshVertices(m), descriptors.MeshIndices(m)
	if vertices.ID() == 0 || m.VertexCount() <= 0 || stride <= 0 {
		return
	}
	// The index buffer is checked before anything is emitted for this draw, so
	// a dropped one leaves no orphaned pipeline or binding behind it. It is the
	// one thing gfx can say about an index buffer without walking it, and
	// MeshIndexed is a pure value constructor with nowhere to say it.
	if m.Indexed() && indices.ID() != 0 && indices.Size()%m.IndexWidth().Bytes() != 0 {
		if err := t.reportIndexLength(m, op.Material.Shader()); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	// Every shader failure is fatal to its draw, whether or not this frame is the
	// one that reports it: the error surfaces once, the draw is dropped every
	// time. ErrShaderExceedsWebLimits stays the only report in gfx that drops
	// nothing at all.
	shaderID, label, err := t.ensureShader(f, op.Material.Shader())
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if shaderID == 0 {
		return
	}
	// A vertex layout that does not supply what the shader reads is fatal to
	// the draw on the same terms a shader failure is: it reports on the frame
	// that built the pipeline and drops the draw on every frame after it.
	pipeline, err := t.ensurePipeline(f.backend, shaderID, label, m, op.Material.State(), pass)
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if pipeline == 0 {
		return
	}

	layout := t.shaderLayout(f.backend, shaderID)
	plan := t.preparePlanFor(shaderID, label, layout, &op.Material, op.Params)
	if plan.mismatch != nil {
		if *firstErr == nil {
			*firstErr = plan.mismatch
		}
		return
	}
	if name, ok := sampledAttachment(plan, op.Params, op.Material.Params(), pass); ok {
		if *firstErr == nil {
			*firstErr = types.ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: name}
		}
		return
	}
	if resource, unbaked, ok := unsuppliedBuffer(plan, op.Params, op.Material.Params()); ok {
		if err := t.reportUnsuppliedBuffer(shaderID, label, resource, unbaked); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	if resource, layers, ok := mismatchedTextureView(plan, op.Params, op.Material.Params()); ok {
		if err := t.reportTextureView(shaderID, label, resource, layers); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	t.ops.SetPipeline(pipeline)
	// Each uniform block the shader declares gets its own slot in the frame's
	// uniform arena, and a shader that declares none gets no uniform binding at
	// all. Emitting one anyway puts an entry in a group the pipeline layout does
	// not have, and CreateBindGroup fails the entry-count rule with the whole
	// frame's command buffer as the casualty.
	for i := range plan.blocks {
		block := &plan.blocks[i]
		packParams(t.ops.SetUniformBlock(block.group, block.binding, block.size), op.Params, op.Material.Params(), block)
	}
	t.emitResources(f, op.Params, op.Material.Params(), plan)
	t.ops.SetVertexBuffer(vertices.ID(), 0)

	instances := op.Instances
	if instances < 1 {
		instances = 1
	}
	if m.Indexed() && indices.ID() != 0 && m.IndexCount() > 0 {
		t.ops.SetIndexBuffer(indices.ID(), 0, m.IndexWidth())
		t.ops.Draw(0, m.IndexCount(), instances, op.FirstInstance, true)
	} else {
		t.ops.Draw(0, m.VertexCount(), instances, op.FirstInstance, false)
	}
}

// reportIndexLength returns the report for a malformed index buffer the first
// time that shape is seen, and nothing on the frames after it. The draw is
// dropped either way: the caller returns before emitting anything, so
// report-once-drop-always holds here the way it does for a failed pipeline.
// The shader's label is spelled only for a report, never on the frames after it.
func (t *translator) reportIndexLength(m *descriptors.MeshDescr, shaderDescr shader.ShaderDescr) error {
	key := indexLengthKey{length: descriptors.MeshIndices(m).Size(), width: m.IndexWidth()}
	if _, seen := t.badIndexLengths[key]; seen {
		return nil
	}
	t.badIndexLengths[key] = struct{}{}
	return types.ErrIndexBufferLength{Shader: shader.ShaderLabel(shaderDescr), Length: key.length, Width: key.width.Bytes()}
}

// unsuppliedBuffer returns the first declared storage binding the draw does not
// fill, and whether a parameter named it at all. It walks the same slice
// sampledAttachment does, for the same reason: the plan is cached per parameter
// shape, so it knows which names are declared but not which buffers this draw
// carries, and an unbaked buffer is a per-draw value.
func unsuppliedBuffer(plan *parameterPlan, drawParams, materialParams []descriptors.ParameterDescr) (*plannedResource, bool, bool) {
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedBuffer {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil {
			return resource, false, true
		}
		// The kind is already settled in plan.mismatch, so a parameter that is
		// here at all is a buffer; only its id is still in question.
		if descriptors.ParameterBuffer(p).ID() == 0 {
			return resource, true, true
		}
	}
	return nil, false, false
}

// reportUnsuppliedBuffer returns the report for an unfilled storage binding the
// first time that binding is seen, and nothing on the frames after it. The draw
// is dropped either way, on reportIndexLength's terms: a material that misses a
// binding misses it until someone fixes the material, and firstErr carries only
// the frame's first error, so re-reporting would mask every later error in
// every later frame.
func (t *translator) reportUnsuppliedBuffer(shaderID types.ShaderID, label string, resource *plannedResource, unbaked bool) error {
	key := unsuppliedBufferKey{shader: shaderID, parameter: resource.name}
	if _, seen := t.unsuppliedBuffers[key]; seen {
		return nil
	}
	t.unsuppliedBuffers[key] = struct{}{}
	return types.ErrStorageBufferUnsupplied{
		Shader: label, Parameter: resource.name,
		Group: resource.group, Binding: resource.binding, Unbaked: unbaked,
	}
}

// mismatchedTextureView returns the first texture binding a draw fills with a
// texture of the wrong view dimension, and the layer count that said so. It
// walks the same slice unsuppliedBuffer does, for the same reason: the declared
// dimension is the plan's, and the layer count facing it is this draw's.
//
// A binding no parameter fills is not a mismatch - that is the white fallback,
// and white is a picture at either dimension. Neither is a descriptor reporting
// zero layers: only an allocation names a count, so zero means the descriptor
// cannot say, and refusing a draw over a descriptor's silence would be a false
// fatal.
func mismatchedTextureView(
	plan *parameterPlan, drawParams, materialParams []descriptors.ParameterDescr,
) (*plannedResource, int, bool) {
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil {
			continue
		}
		// The kind is already settled in plan.mismatch, so a parameter that is
		// here at all is a texture; only its shape is still in question.
		layers := descriptors.ParameterTexture(p).Layers()
		if layers == 0 {
			continue
		}
		if (layers > 1) != (resource.view == shader.TextureView2DArray) {
			return resource, layers, true
		}
	}
	return nil, 0, false
}

// reportTextureView returns the report for a texture binding filled at the
// wrong dimension the first time that binding is seen, and nothing on the
// frames after it. The draw is dropped either way, on reportUnsuppliedBuffer's
// terms.
func (t *translator) reportTextureView(
	shaderID types.ShaderID, label string, resource *plannedResource, layers int,
) error {
	key := textureViewKey{shader: shaderID, parameter: resource.name}
	if _, seen := t.textureViewMismatches[key]; seen {
		return nil
	}
	t.textureViewMismatches[key] = struct{}{}
	return types.ErrTextureViewDimensionMismatch{
		Shader: label, Parameter: resource.name,
		Group: resource.group, Binding: resource.binding,
		Declared: textureViewName(resource.view), Supplied: textureViewNameOfLayers(layers),
	}
}

// textureViewName renders a declared dimension the way the shader spells it, so
// the report can be matched against the source it names.
func textureViewName(view shader.TextureViewDimension) string {
	if view == shader.TextureView2DArray {
		return "texture_2d_array"
	}
	return "texture_2d"
}

// textureViewNameOfLayers renders what a texture's layer count makes it, which
// is how a supplied texture's dimension is known: more than one layer is an
// array texture and one is flat.
func textureViewNameOfLayers(layers int) string {
	if layers > 1 {
		return "texture_2d_array"
	}
	return "single-layer"
}

// sampledAttachment names the first texture parameter a draw samples that its
// own pass renders into. Only a baked texture can be an attachment, so this
// resolves nothing and costs a comparison per binding.
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []descriptors.ParameterDescr, pass descriptors.PassDescr) (string, bool) {
	attachment := func(id types.TextureID) bool {
		if id == 0 {
			return false
		}
		return (descriptors.TargetKindOf(&pass.Target) == descriptors.TargetTexture && descriptors.TargetTextureOf(&pass.Target) == id) ||
			(descriptors.DepthKindOf(&pass.Depth) == descriptors.DepthKindTexture && descriptors.DepthTexture(&pass.Depth) == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil || descriptors.ParameterKind(p) != descriptors.ParamTexture {
			continue
		}
		if texture := descriptors.ParameterTextureRef(p); attachment(texture.ID()) {
			return p.Name(), true
		}
	}
	return "", false
}

// emitResources binds each reflected texture/sampler resource, matching its name
// to a material parameter (defaulting to the white texture / a clamp+linear
// sampler when unset), so every binding the shader declares is provided.
func (t *translator) emitResources(f *frame, drawParams, materialParams []descriptors.ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc types.SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && descriptors.ParameterKind(p) == descriptors.ParamSampler {
			desc = descriptors.ParameterSampler(p)
		}
		t.ops.SetSampler(t.ensureSampler(f.backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && descriptors.ParameterKind(p) == descriptors.ParamBuffer {
				if descriptors.ParameterBuffer(p).ID() != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, descriptors.ParameterBuffer(p).ID(), descriptors.ParameterBufferOffset(p), descriptors.ParameterBufferSize(p))
				}
			}
			continue
		}
		textureID := types.TextureID(0)
		if p != nil && descriptors.ParameterKind(p) == descriptors.ParamTexture {
			textureID = t.ensureTexture(f, descriptors.ParameterTexture(p))
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

// preparePlanFor is prepareParameterPlan for one draw's material, taking the
// material half of the shape hash from a material recorded for the frame
// rather than hashing its names again.
func (t *translator) preparePlanFor(
	shaderID types.ShaderID, label string, layout shader.ShaderLayout, material *descriptors.MaterialDescr, draw []descriptors.ParameterDescr,
) *parameterPlan {
	state, recorded := descriptors.MaterialShapeState(material)
	if !recorded {
		state = descriptors.ParameterShapeState(material.Params())
	}
	return t.planForShape(shaderID, label, layout, material.Params(), draw, descriptors.ContinueParameterShape(state, draw))
}

func (t *translator) prepareParameterPlan(shaderID types.ShaderID, label string, layout shader.ShaderLayout, material, draw []descriptors.ParameterDescr) *parameterPlan {
	return t.planForShape(shaderID, label, layout, material, draw, parameterShapeHash(material, draw))
}

// planForShape finds or builds the plan for one parameter shape, given its
// hash.
func (t *translator) planForShape(
	shaderID types.ShaderID, label string, layout shader.ShaderLayout, material, draw []descriptors.ParameterDescr, hash uint64,
) *parameterPlan {
	key := parameterPlanBucketKey{shader: shaderID, hash: hash}
	bucket := t.parameterPlans[key]
	for i := range bucket {
		if parameterShapeEqual(&bucket[i], material, draw) {
			return &bucket[i].plan
		}
	}

	entry := cachedParameterPlan{
		materialNames: parameterNames(material),
		drawNames:     parameterNames(draw),
	}
	entry.plan.resources = make([]plannedResource, 0, len(layout.Resources))
	for i := range layout.Resources {
		resource := &layout.Resources[i]
		kind, declared := plannedTexture, declaredTexture
		switch resource.Kind.Base() {
		case shader.ResourceUniformBuffer:
			// A uniform block is packed member by member rather than bound by
			// name.
			block := plannedBlock{
				group: resource.Group, binding: resource.Binding, size: resource.Size,
				members: make([]plannedUniform, len(resource.Members)),
			}
			for j := range resource.Members {
				member := &resource.Members[j]
				ref := parameterRefFor(member.Name, material, draw)
				entry.plan.checkKind(label, member.Name, ref, material, draw, declaredValue)
				block.members[j] = plannedUniform{offset: member.Offset, param: ref}
			}
			entry.plan.blocks = append(entry.plan.blocks, block)
			continue
		case shader.ResourceSampler:
			ref := parameterRefFor(resource.Name, material, draw)
			entry.plan.checkKind(label, resource.Name, ref, material, draw, declaredSampler)
			entry.plan.samplers = append(entry.plan.samplers, plannedSampler{
				group: resource.Group, binding: resource.Binding, param: ref,
			})
			continue
		case shader.ResourceStorageBuffer:
			kind, declared = plannedBuffer, declaredBuffer
		}
		ref := parameterRefFor(resource.Name, material, draw)
		entry.plan.checkKind(label, resource.Name, ref, material, draw, declared)
		entry.plan.resources = append(entry.plan.resources, plannedResource{
			kind: kind, group: resource.Group, binding: resource.Binding, param: ref, name: resource.Name,
			view: resource.TextureView,
		})
	}

	bucket = append(bucket, entry)
	t.parameterPlans[key] = bucket
	return &bucket[len(bucket)-1].plan
}

// packParams writes reflected shader constants into buf, which arrives zeroed
// and sized to the planned block. Per-draw parameters override same-named
// material parameters; unmatched members remain zero.
func packParams(buf []byte, drawParams, materialParams []descriptors.ParameterDescr, block *plannedBlock) {
	size := min(block.size, len(buf))
	if size <= 0 {
		return
	}
	buf = buf[:size]
	for i := range block.members {
		uniform := &block.members[i]
		off := uniform.offset
		if off < 0 || off >= size {
			continue
		}
		if p := uniform.param.value(materialParams, drawParams); p != nil {
			writeParamAt(buf, off, p)
		}
	}
}

// writeParamAt writes a scalar/vec/color param at byte offset off (bounds-checked).
func writeParamAt(buf []byte, off int, p *descriptors.ParameterDescr) {
	switch descriptors.ParameterKind(p) {
	case descriptors.ParamColor:
		if off+16 <= len(buf) {
			descriptors.WriteColor(buf[off:off+16], descriptors.ParameterColor(p))
		}
	case descriptors.ParamVec4:
		if off+16 <= len(buf) {
			descriptors.WriteVec4(buf[off:off+16], descriptors.ParameterVec(p))
		}
	case descriptors.ParamMat4:
		if off+64 <= len(buf) {
			descriptors.WriteMat4(buf[off:off+64], descriptors.ParameterMat(p))
		}
	case descriptors.ParamFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(descriptors.ParameterNum(p)))
		}
	case descriptors.ParamRaw:
		raw := descriptors.ParameterRaw(p)
		if off+raw.Len() <= len(buf) {
			copy(buf[off:], raw.Data())
		}
	}
}
