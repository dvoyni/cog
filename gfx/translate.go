package gfx

import (
	"cmp"
	"encoding/binary"
	"math"
	"slices"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/storage"
)

// uniformMax caps a per-draw shader-parameter block.
const uniformMax = 256

// pipelineKey identifies a cached pipeline by shader identity, render state and
// the formats of the attachments it renders into. MaterialState is embedded
// whole so that adding a state field cannot silently return a pipeline built
// for the old one.
type pipelineKey struct {
	shader      ShaderID
	topology    PrimitiveTopology
	state       MaterialState
	colorFormat TextureFormat
	depthFormat TextureFormat
	layout      vertexLayoutKey
}

// translator turns an OpQueue into a GpuQueue, lazily creating and caching
// backend shaders/pipelines/samplers and resolves every texture and buffer to a
// baked resource ID. It is owned by the plugin and used only on the driver's
// render thread inside ConsumeCmd.
type translator struct {
	shaders        map[ShaderDescr]ShaderID
	pipelines      map[pipelineKey]PipelineID
	samplers       map[SamplerDesc]SamplerID
	uarena         []byte
	layouts        map[ShaderID]ShaderLayout
	textures       map[textureKey]TextureDescr
	parameterPlans map[parameterPlanBucketKey][]cachedParameterPlan
	ops            GpuQueue
	// Pass bookkeeping, reused each frame: the run order of the frame's passes
	// and its draws bucketed behind the pass that recorded them.
	passOrder  []int
	passStart  []int
	passDraws  []int
	passCursor []int
	// strayDraws counts the frame's draws recorded outside any pass.
	strayDraws int
	// diagnostic holds a report that does not stop the frame - a shader over the
	// web floor still renders here - until translate surfaces it.
	diagnostic error
}

func newTranslator() *translator {
	return &translator{
		shaders:        map[ShaderDescr]ShaderID{},
		pipelines:      map[pipelineKey]PipelineID{},
		samplers:       map[SamplerDesc]SamplerID{},
		layouts:        map[ShaderID]ShaderLayout{},
		textures:       map[textureKey]TextureDescr{},
		parameterPlans: map[parameterPlanBucketKey][]cachedParameterPlan{},
	}
}

// translate builds the op stream for the durable resource operations followed
// by the latest frame. The returned ops (and their payloads) are valid until
// the next translate call. It returns the first error encountered; valid draws
// are still translated.
func (t *translator) translate(queue *OpQueue, persistent []op, backend Backend, filesystem storage.FileSystem) (*GpuQueue, error) {
	t.ops.Reset()

	need := len(queue.ops) * uniformMax
	if cap(t.uarena) < need {
		t.uarena = make([]byte, need)
	}
	t.uarena = t.uarena[:need]

	var firstErr error
	uoff := 0
	// Resource ops belong to no pass: every bake is hoisted ahead of all of
	// them, so a pass can read anything the frame uploaded.
	translateResources := func(list []op) {
		for i := range list {
			op := &list[i]
			if op.kind == opBakeBuffer {
				if len(op.bytes) > 0 {
					t.ops.BakeBuffer(op.bufferID, op.bufferKind, op.bufferSize, op.bytes)
				}
				continue
			}
			if op.kind == opReleaseBuffer {
				t.ops.ReleaseBuffer(op.bufferID)
				continue
			}
			if op.kind == opBakeTexture {
				if len(op.bytes) > 0 {
					t.ops.BakeTexture(op.textureID, op.texW, op.texH, op.format, op.bytes, op.mipmaps)
				}
				continue
			}
			if op.kind == opReleaseTexture {
				t.ops.ReleaseTexture(op.textureID)
				continue
			}
			if op.kind == opReleaseCachedResource {
				t.releaseCachedResource(backend, op.path)
				continue
			}
			if op.kind == opFreeCachedResources {
				t.freeCachedResources(backend)
				continue
			}
			if op.kind == opAllocateTexture {
				t.ops.AllocateTexture(op.textureID, TextureDesc{
					Width: op.texW, Height: op.texH, Layers: op.texLayers, Format: op.format,
					Renderable: op.renderable,
				})
				continue
			}
			if op.kind == opUpdateTexture {
				t.ops.UpdateTexture(op.textureID, op.texLayer, op.region, op.bytes)
				continue
			}
		}
	}
	translateResources(persistent)
	translateResources(queue.ops)

	t.translatePasses(queue, backend, filesystem, &uoff, &firstErr)

	// A report that did not stop anything is still worth surfacing, but only
	// behind an error that did.
	if firstErr == nil {
		firstErr = t.diagnostic
	}
	t.diagnostic = nil

	return &t.ops, firstErr
}

// translatePasses runs the frame's passes in Order, merging the runs that are
// indistinguishable from one longer pass, and emits each one's draws.
func (t *translator) translatePasses(queue *OpQueue, backend Backend, filesystem storage.FileSystem, uoff *int, firstErr *error) {
	t.planPasses(queue)
	if t.strayDraws > 0 && *firstErr == nil {
		*firstErr = ErrDrawWithoutPass{Count: t.strayDraws}
	}
	presents := false
	for i := 0; i < len(t.passOrder); {
		head := queue.passes[t.passOrder[i]].desc
		tail, draws := head, t.passDrawCount(t.passOrder[i])
		last := i
		for j := i + 1; j < len(t.passOrder); j++ {
			next := queue.passes[t.passOrder[j]].desc
			if !mergesInto(next, tail) {
				break
			}
			tail, last = next, j
			draws += t.passDrawCount(t.passOrder[j])
		}
		if !head.hasEffect(draws) {
			i = last + 1
			continue
		}
		presents = presents || head.Target.IsScreen()
		t.ops.BeginPass(t.gpuPassDesc(backend, head, tail))
		for j := i; j <= last; j++ {
			pass := &queue.passes[t.passOrder[j]]
			for _, index := range t.passDrawOps(t.passOrder[j]) {
				t.translateDraw(&queue.ops[index], pass.desc, backend, filesystem, uoff, firstErr)
			}
		}
		t.ops.EndPass()
		i = last + 1
	}
	// The present pass exists only to show the frame buffer, so it is emitted
	// exactly when the frame buffer was used - which is also when the backend
	// allocated it. A frame that rendered only into its own textures leaves the
	// screen alone rather than blitting a buffer nothing wrote to.
	if presents {
		t.ops.Present()
	}
}

// gpuPassDesc resolves a merged run's attachments: it loads like the pass that
// opened the run and stores like the one that closed it.
func (t *translator) gpuPassDesc(backend Backend, head, tail PassDescr) GpuPassDesc {
	desc := GpuPassDesc{
		Load: head.Load, Clear: head.Clear, Store: tail.Store,
		DepthLoad: head.DepthLoad, DepthClear: head.DepthClear, DepthStore: tail.DepthStore,
		Label: head.Label,
	}
	switch head.Target.kind {
	case targetScreen:
		desc.Screen = true
	case targetTexture:
		desc.Target = backend.TextureView(head.Target.texture, head.Target.mip, head.Target.layer)
	}
	switch head.Depth.kind {
	case depthKindAuto:
		desc.DepthAuto = true
	case depthKindTexture:
		desc.Depth = backend.TextureView(head.Depth.texture, 0, 0)
	}
	return desc
}

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(op *op, pass PassDescr, backend Backend, filesystem storage.FileSystem, uoff *int, firstErr *error) {
	m := &op.mesh
	stride := m.stride()
	if m.vertices.id == 0 || m.vertexCount <= 0 || stride <= 0 {
		return
	}
	shaderID, err := t.ensureShader(backend, filesystem, op.material.shader)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return
	}
	pipeline := t.ensurePipeline(backend, shaderID, m, op.material.state)
	if pipeline == 0 {
		return
	}

	layout := t.shaderLayout(backend, shaderID)
	plan := t.prepareParameterPlan(shaderID, layout, op.material.params, op.params)
	if name, ok := sampledAttachment(plan, op.params, op.material.params, pass); ok {
		if *firstErr == nil {
			*firstErr = ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: name}
		}
		return
	}
	u := t.packParams(t.uarena[*uoff:*uoff+uniformMax], op.params, op.material.params, plan)
	*uoff += uniformMax

	t.ops.SetPipeline(pipeline)
	t.ops.SetParams(u)
	t.emitResources(backend, filesystem, op.params, op.material.params, plan)
	t.ops.SetVertexBuffer(m.vertices.id, 0)

	instances := op.instances
	if instances < 1 {
		instances = 1
	}
	if m.indexed && m.indices.id != 0 && m.indexCount > 0 {
		t.ops.SetIndexBuffer(m.indices.id, 0)
		t.ops.Draw(0, m.indexCount, instances, op.firstInstance, true)
	} else {
		t.ops.Draw(0, m.vertexCount, instances, op.firstInstance, false)
	}
}

// sampledAttachment names the first texture parameter a draw samples that its
// own pass renders into. Only a baked texture can be an attachment, so this
// resolves nothing and costs a comparison per binding.
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []ParameterDescr, pass PassDescr) (string, bool) {
	attachment := func(id TextureID) bool {
		if id == 0 {
			return false
		}
		return (pass.Target.kind == targetTexture && pass.Target.texture == id) ||
			(pass.Depth.kind == depthKindTexture && pass.Depth.texture == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p != nil && p.kind == paramTexture && p.texture.source == TextureSourceBaked && attachment(p.texture.id) {
			return p.name, true
		}
	}
	return "", false
}

// planPasses puts the frame's passes in run order - Order first, declaration
// sequence breaking ties - and buckets each pass's draws behind it.
func (t *translator) planPasses(queue *OpQueue) {
	count := len(queue.passes)
	t.passOrder = t.passOrder[:0]
	for i := range count {
		t.passOrder = append(t.passOrder, i)
	}
	slices.SortStableFunc(t.passOrder, func(a, b int) int {
		return cmp.Compare(queue.passes[a].desc.Order, queue.passes[b].desc.Order)
	})

	t.passStart = slices.Grow(t.passStart[:0], count+1)[:count+1]
	clear(t.passStart)
	t.strayDraws = 0
	for i := range queue.ops {
		if queue.ops[i].kind != opDraw {
			continue
		}
		if pass := int(queue.ops[i].pass); pass >= 0 && pass < count {
			t.passStart[pass+1]++
		} else {
			t.strayDraws++
		}
	}
	for i := 1; i <= count; i++ {
		t.passStart[i] += t.passStart[i-1]
	}
	t.passDraws = slices.Grow(t.passDraws[:0], t.passStart[count])[:t.passStart[count]]
	cursor := append(t.passCursor[:0], t.passStart[:count]...)
	for i := range queue.ops {
		if pass := int(queue.ops[i].pass); queue.ops[i].kind == opDraw && pass >= 0 && pass < count {
			t.passDraws[cursor[pass]] = i
			cursor[pass]++
		}
	}
	t.passCursor = cursor
}

func (t *translator) passDrawOps(pass int) []int {
	return t.passDraws[t.passStart[pass]:t.passStart[pass+1]]
}

func (t *translator) passDrawCount(pass int) int {
	return t.passStart[pass+1] - t.passStart[pass]
}

// emitResources binds each reflected texture/sampler resource, matching its name
// to a material parameter (defaulting to the white texture / a clamp+linear
// sampler when unset), so every binding the shader declares is provided.
func (t *translator) emitResources(backend Backend, filesystem storage.FileSystem, drawParams, materialParams []ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && p.kind == paramSampler {
			desc = p.sampler
		}
		t.ops.SetSampler(t.ensureSampler(backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && p.kind == paramBuffer {
				if p.buffer.id != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, p.buffer.id, p.bufferOffset, p.bufferSize)
				}
			}
			continue
		}
		textureID := TextureID(0)
		if p != nil && p.kind == paramTexture {
			textureID = t.ensureTexture(backend, filesystem, p.texture)
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

func (t *translator) ensureTexture(backend Backend, filesystem storage.FileSystem, descr TextureDescr) TextureID {
	if descr.source == TextureSourceBaked {
		return descr.id
	}
	if descr.source != TextureSourceResource {
		return 0
	}
	key := textureKeyOf(descr)
	if baked, ok := t.textures[key]; ok {
		return baked.id
	}
	width, height, pixels, ok := loadTextureResource(filesystem, descr.path)
	if !ok {
		return 0
	}
	id := backend.NewTexture()
	t.ops.BakeTexture(id, width, height, descr.format, pixels, false)
	t.textures[key] = TextureDescr{source: TextureSourceBaked, id: id}
	return id
}

func (t *translator) ensureShader(backend Backend, filesystem storage.FileSystem, descr ShaderDescr) (ShaderID, error) {
	if id, ok := t.shaders[descr]; ok {
		return id, nil
	}
	code, err := t.shaderCode(filesystem, descr)
	if err != nil {
		return 0, err
	}
	label := shaderLabel(descr)
	id, err := backend.NewShader(ShaderDesc{Code: code, Label: label})
	if err != nil {
		return 0, err
	}
	t.shaders[descr] = id
	// Every shader gfx reflects is measured, not only an engine's bundled ones:
	// a caller-supplied material is what actually gets bound at draw time. The
	// shader is cached, so this reports once rather than once a frame.
	if diagnostic := checkWebLimits(label, t.shaderLayout(backend, id), backend.Limits()); diagnostic != nil && t.diagnostic == nil {
		t.diagnostic = diagnostic
	}
	return id, nil
}

func (t *translator) releaseCachedResource(backend Backend, path string) {
	// One path can be cached once per format, and the caller names a file.
	for key, texture := range t.textures {
		if key.path == path {
			t.ops.ReleaseTexture(texture.id)
			delete(t.textures, key)
		}
	}
	shaderDescr := ShaderWithResource(path)
	if shader, ok := t.shaders[shaderDescr]; ok {
		t.releaseShader(backend, shaderDescr, shader)
	}
}

func (t *translator) releaseShader(backend Backend, descr ShaderDescr, shader ShaderID) {
	for key, pipeline := range t.pipelines {
		if key.shader == shader {
			backend.FreePipeline(pipeline)
			delete(t.pipelines, key)
		}
	}
	for key := range t.parameterPlans {
		if key.shader == shader {
			delete(t.parameterPlans, key)
		}
	}
	delete(t.layouts, shader)
	backend.FreeShader(shader)
	delete(t.shaders, descr)
}

func (t *translator) freeCachedResources(backend Backend) {
	for _, texture := range t.textures {
		t.ops.ReleaseTexture(texture.id)
	}
	for _, pipeline := range t.pipelines {
		backend.FreePipeline(pipeline)
	}
	for _, shader := range t.shaders {
		backend.FreeShader(shader)
	}
	for _, sampler := range t.samplers {
		backend.FreeSampler(sampler)
	}
	clear(t.textures)
	clear(t.pipelines)
	clear(t.shaders)
	clear(t.samplers)
	clear(t.layouts)
	clear(t.parameterPlans)
}

// shaderCode resolves inline source directly or reads a resource from storage.
func (t *translator) shaderCode(filesystem storage.FileSystem, descr ShaderDescr) ([]byte, error) {
	if descr.source == ShaderSourceResource {
		if code, ok := loadShaderResource(filesystem, descr.textOrPath); ok {
			return code, nil
		}
		return nil, ErrShaderNotFound{Name: descr.textOrPath}
	}
	return []byte(descr.textOrPath), nil
}

func shaderLabel(descr ShaderDescr) string {
	if descr.source == ShaderSourceResource {
		return descr.textOrPath
	}
	return "gfx.shader"
}

// shaderLayout returns the backend's reflected layout for a shader, cached by id.
func (t *translator) shaderLayout(backend Backend, id ShaderID) ShaderLayout {
	if l, ok := t.layouts[id]; ok {
		return l
	}
	l := backend.ShaderLayout(id)
	t.layouts[id] = l
	return l
}

// packParams writes reflected shader constants into dst. Per-draw parameters
// override same-named material parameters; unmatched members remain zero.
func (t *translator) packParams(dst []byte, drawParams, materialParams []ParameterDescr, plan *parameterPlan) []byte {
	size := plan.uniformSize
	if size <= 0 {
		return dst[:0]
	}
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
func writeParamAt(buf []byte, off int, p *ParameterDescr) {
	switch p.kind {
	case paramColor:
		if off+16 <= len(buf) {
			writeColor(buf[off:off+16], p.color)
		}
	case paramVec4:
		if off+16 <= len(buf) {
			writeVec4(buf[off:off+16], p.vec)
		}
	case paramMat4:
		if off+64 <= len(buf) {
			writeMat4(buf[off:off+64], p.mat)
		}
	case paramFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(p.num))
		}
	}
}

func (t *translator) ensurePipeline(backend Backend, shader ShaderID, m *MeshDescr, state MaterialState) PipelineID {
	stride := m.stride()
	layout, ok := vertexLayoutKeyOf(m.layout)
	if !ok {
		return 0
	}
	// One target exists today, the screen, and its depth buffer. Explicit passes
	// are what will hand these formats in instead of naming them here.
	const colorFormat, depthFormat = FormatScreen, FormatDepth32F
	k := pipelineKey{
		shader: shader, topology: m.topology, state: state,
		colorFormat: colorFormat, depthFormat: depthFormat, layout: layout,
	}
	if id, ok := t.pipelines[k]; ok {
		return id
	}
	attrs := make([]VertexAttribute, len(m.layout))
	for i := range m.layout {
		attrs[i] = VertexAttribute{Offset: m.layout[i].offset, Type: m.layout[i].typ, Location: i}
	}
	id, err := backend.NewPipeline(PipelineDesc{
		Shader:      shader,
		Topology:    m.topology,
		State:       state,
		ColorFormat: colorFormat,
		DepthFormat: depthFormat,
		Stride:      stride,
		Attributes:  attrs,
		Label:       "gfx.pipeline",
	})
	if err != nil {
		return 0
	}
	t.pipelines[k] = id
	return id
}

func (t *translator) ensureSampler(backend Backend, desc SamplerDesc) SamplerID {
	if id, ok := t.samplers[desc]; ok {
		return id
	}
	id, err := backend.NewSampler(desc)
	if err != nil {
		return 0
	}
	t.samplers[desc] = id
	return id
}

func writeMat4(dst []byte, m m.Mat4) {
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(m[i]))
	}
}

func writeColor(dst []byte, c m.Color) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(c.R))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(c.G))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(c.B))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(c.A))
}

func writeVec4(dst []byte, v m.Vec4) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(v.W))
}
