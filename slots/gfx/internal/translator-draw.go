package internal

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(f *frame, op *types.Op, pass gfx.PassDescr, uoff *int, firstErr *error) {
	m := &op.Mesh
	stride := types.MeshStride(m)
	vertices, indices := types.MeshVertices(m), types.MeshIndices(m)
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
	plan := t.prepareParameterPlan(shaderID, label, layout, op.Material.Params(), op.Params)
	if plan.mismatch != nil {
		if *firstErr == nil {
			*firstErr = plan.mismatch
		}
		return
	}
	if name, ok := sampledAttachment(plan, op.Params, op.Material.Params(), pass); ok {
		if *firstErr == nil {
			*firstErr = gfx.ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: name}
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
	// A shader that declares no uniform block gets no uniform binding and no
	// slot in the backend's arena. Emitting one anyway puts an entry in a group
	// the pipeline layout does not have, and CreateBindGroup fails the
	// entry-count rule with the whole frame's command buffer as the casualty.
	if plan.uniformSize > 0 {
		u := t.packParams(t.uarena[*uoff:*uoff+uniformMax], op.Params, op.Material.Params(), plan)
		*uoff += uniformMax
		t.ops.SetParams(u)
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
func (t *translator) reportIndexLength(m *gfx.MeshDescr, shader gfx.ShaderDescr) error {
	key := indexLengthKey{length: types.MeshIndices(m).Size(), width: m.IndexWidth()}
	if _, seen := t.badIndexLengths[key]; seen {
		return nil
	}
	t.badIndexLengths[key] = struct{}{}
	return gfx.ErrIndexBufferLength{Shader: types.ShaderLabel(shader), Length: key.length, Width: key.width.Bytes()}
}

// unsuppliedBuffer returns the first declared storage binding the draw does not
// fill, and whether a parameter named it at all. It walks the same slice
// sampledAttachment does, for the same reason: the plan is cached per parameter
// shape, so it knows which names are declared but not which buffers this draw
// carries, and an unbaked buffer is a per-draw value.
func unsuppliedBuffer(plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr) (*plannedResource, bool, bool) {
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
		if types.ParameterBuffer(p).ID() == 0 {
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
func (t *translator) reportUnsuppliedBuffer(shader gfx.ShaderID, label string, resource *plannedResource, unbaked bool) error {
	key := unsuppliedBufferKey{shader: shader, parameter: resource.name}
	if _, seen := t.unsuppliedBuffers[key]; seen {
		return nil
	}
	t.unsuppliedBuffers[key] = struct{}{}
	return gfx.ErrStorageBufferUnsupplied{
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
	plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr,
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
		layers := types.ParameterTexture(p).Layers()
		if layers == 0 {
			continue
		}
		if (layers > 1) != (resource.view == gfx.TextureView2DArray) {
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
	shader gfx.ShaderID, label string, resource *plannedResource, layers int,
) error {
	key := textureViewKey{shader: shader, parameter: resource.name}
	if _, seen := t.textureViewMismatches[key]; seen {
		return nil
	}
	t.textureViewMismatches[key] = struct{}{}
	return gfx.ErrTextureViewDimensionMismatch{
		Shader: label, Parameter: resource.name,
		Group: resource.group, Binding: resource.binding,
		Declared: textureViewName(resource.view), Supplied: textureViewNameOfLayers(layers),
	}
}

// textureViewName renders a declared dimension the way the shader spells it, so
// the report can be matched against the source it names.
func textureViewName(view gfx.TextureViewDimension) string {
	if view == gfx.TextureView2DArray {
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
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr, pass gfx.PassDescr) (string, bool) {
	attachment := func(id gfx.TextureID) bool {
		if id == 0 {
			return false
		}
		return (types.TargetKindOf(&pass.Target) == types.TargetTexture && types.TargetTextureOf(&pass.Target) == id) ||
			(types.DepthKindOf(&pass.Depth) == types.DepthKindTexture && types.DepthTexture(&pass.Depth) == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil || types.ParameterKind(p) != types.ParamTexture {
			continue
		}
		if texture := types.ParameterTextureRef(p); attachment(texture.ID()) {
			return p.Name(), true
		}
	}
	return "", false
}

// emitResources binds each reflected texture/sampler resource, matching its name
// to a material parameter (defaulting to the white texture / a clamp+linear
// sampler when unset), so every binding the shader declares is provided.
func (t *translator) emitResources(f *frame, drawParams, materialParams []gfx.ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc gfx.SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && types.ParameterKind(p) == types.ParamSampler {
			desc = types.ParameterSampler(p)
		}
		t.ops.SetSampler(t.ensureSampler(f.backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && types.ParameterKind(p) == types.ParamBuffer {
				if types.ParameterBuffer(p).ID() != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, types.ParameterBuffer(p).ID(), types.ParameterBufferOffset(p), types.ParameterBufferSize(p))
				}
			}
			continue
		}
		textureID := gfx.TextureID(0)
		if p != nil && types.ParameterKind(p) == types.ParamTexture {
			textureID = t.ensureTexture(f, types.ParameterTexture(p))
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

func (t *translator) prepareParameterPlan(shader gfx.ShaderID, label string, layout gfx.ShaderLayout, material, draw []gfx.ParameterDescr) *parameterPlan {
	key := parameterPlanBucketKey{shader: shader, hash: parameterShapeHash(material, draw)}
	bucket := t.parameterPlans[key]
	for i := range bucket {
		if parameterShapeEqual(&bucket[i], material, draw) {
			return &bucket[i].plan
		}
	}

	entry := cachedParameterPlan{
		materialNames: parameterNames(material),
		drawNames:     parameterNames(draw),
		plan:          parameterPlan{uniformSize: layout.UniformSize},
	}
	entry.plan.uniforms = make([]plannedUniform, len(layout.Uniforms))
	for i := range layout.Uniforms {
		member := &layout.Uniforms[i]
		ref := parameterRefFor(member.Name, material, draw)
		entry.plan.checkKind(label, member.Name, ref, material, draw, declaredValue)
		entry.plan.uniforms[i] = plannedUniform{offset: member.Offset, param: ref}
	}
	entry.plan.resources = make([]plannedResource, 0, len(layout.Resources))
	for i := range layout.Resources {
		resource := &layout.Resources[i]
		ref := parameterRefFor(resource.Name, material, draw)
		if resource.Sampler {
			entry.plan.checkKind(label, resource.Name, ref, material, draw, declaredSampler)
			entry.plan.samplers = append(entry.plan.samplers, plannedSampler{
				group: resource.Group, binding: resource.Binding, param: ref,
			})
			continue
		}
		kind, declared := plannedTexture, declaredTexture
		if resource.StorageBuffer {
			kind, declared = plannedBuffer, declaredBuffer
		}
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

// packParams writes reflected shader constants into dst. Per-draw parameters
// override same-named material parameters; unmatched members remain zero.
func (t *translator) packParams(dst []byte, drawParams, materialParams []gfx.ParameterDescr, plan *parameterPlan) []byte {
	size := plan.uniformSize
	if size <= 0 {
		return dst[:0]
	}
	// A guard only: checkUniformBlock refuses any shader whose block exceeds
	// uniformMax when it is loaded, so no plan reaching here is larger than dst.
	if size > len(dst) {
		size = len(dst)
	}
	buf := dst[:size]
	clear(buf)
	for i := range plan.uniforms {
		uniform := &plan.uniforms[i]
		off := uniform.offset
		if off < 0 || off >= size {
			continue
		}
		if p := uniform.param.value(materialParams, drawParams); p != nil {
			writeParamAt(buf, off, p)
		}
	}
	return buf
}

// writeParamAt writes a scalar/vec/color param at byte offset off (bounds-checked).
func writeParamAt(buf []byte, off int, p *gfx.ParameterDescr) {
	switch types.ParameterKind(p) {
	case types.ParamColor:
		if off+16 <= len(buf) {
			types.WriteColor(buf[off:off+16], types.ParameterColor(p))
		}
	case types.ParamVec4:
		if off+16 <= len(buf) {
			types.WriteVec4(buf[off:off+16], types.ParameterVec(p))
		}
	case types.ParamMat4:
		if off+64 <= len(buf) {
			types.WriteMat4(buf[off:off+64], types.ParameterMat(p))
		}
	case types.ParamFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(types.ParameterNum(p)))
		}
	case types.ParamRaw:
		raw := types.ParameterRaw(p)
		if off+raw.Len() <= len(buf) {
			copy(buf[off:], raw.Data())
		}
	}
}
