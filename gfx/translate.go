package gfx

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/storage"
)

// uniformMax caps a per-draw shader-parameter block.
const uniformMax = 256

// pipelineKey identifies a cached pipeline by shader identity and render state.
type pipelineKey struct {
	shader    ShaderID
	topology  PrimitiveTopology
	blend     BlendMode
	depthTest bool
	layout    vertexLayoutKey
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
	translateOps := func(list []op) {
		for i := range list {
			op := &list[i]
			if op.kind == opClear {
				t.ops.Clear(op.color)
				continue
			}
			if op.kind == opClearDepth {
				t.ops.ClearDepth(op.depth)
				continue
			}
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
				})
				continue
			}
			if op.kind == opUpdateTexture {
				t.ops.UpdateTexture(op.textureID, op.texLayer, op.region, op.bytes)
				continue
			}

			m := &op.mesh
			stride := m.stride()
			if m.vertices.id == 0 || m.vertexCount <= 0 || stride <= 0 {
				continue
			}
			shaderID, err := t.ensureShader(backend, filesystem, op.material.shader)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			pipeline := t.ensurePipeline(backend, shaderID, m, op.material.state)
			if pipeline == 0 {
				continue
			}

			layout := t.shaderLayout(backend, shaderID)
			plan := t.prepareParameterPlan(shaderID, layout, op.material.params, op.params)
			u := t.packParams(t.uarena[uoff:uoff+uniformMax], op.params, op.material.params, plan)
			uoff += uniformMax

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
				t.ops.Draw(0, m.indexCount, instances, true)
			} else {
				t.ops.Draw(0, m.vertexCount, instances, false)
			}
		}
	}
	translateOps(persistent)
	translateOps(queue.ops)

	return &t.ops, firstErr
}

// emitResources binds each reflected texture/sampler resource, matching its name
// to a material parameter (defaulting to the white texture / a clamp+linear
// sampler when unset), so every binding the shader declares is provided.
func (t *translator) emitResources(backend Backend, filesystem storage.FileSystem, drawParams, materialParams []ParameterDescr, plan *parameterPlan) {
	// One sampler shared across textures (common case): the first sampler resource.
	sampID := SamplerID(0)
	if plan.samplerBinding >= 0 {
		desc := SamplerDesc{Address: AddressClamp}
		if p := plan.sampler.value(materialParams, drawParams); p != nil && p.kind == paramSampler {
			desc = p.sampler
		}
		sampID = t.ensureSampler(backend, desc)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && p.kind == paramBuffer {
				if p.buffer.id != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, p.buffer.id, 0, 0)
				}
			}
			continue
		}
		textureID := TextureID(0)
		if p != nil && p.kind == paramTexture {
			textureID = t.ensureTexture(backend, filesystem, p.texture)
		}
		t.ops.SetTexture(textureID, sampID, resource.group, resource.binding, plan.samplerBinding)
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
	t.ops.BakeTexture(id, width, height, FormatRGBA8, pixels, false)
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
	id, err := backend.NewShader(ShaderDesc{Code: code, Label: shaderLabel(descr)})
	if err != nil {
		return 0, err
	}
	t.shaders[descr] = id
	return id, nil
}

func (t *translator) releaseCachedResource(backend Backend, path string) {
	textureKey := textureKey(path)
	if texture, ok := t.textures[textureKey]; ok {
		t.ops.ReleaseTexture(texture.id)
		delete(t.textures, textureKey)
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
	k := pipelineKey{
		shader: shader, topology: m.topology, blend: state.Blend, depthTest: state.DepthTest,
		layout: layout,
	}
	if id, ok := t.pipelines[k]; ok {
		return id
	}
	attrs := make([]VertexAttribute, len(m.layout))
	for i := range m.layout {
		attrs[i] = VertexAttribute{Offset: m.layout[i].offset, Type: m.layout[i].typ, Location: i}
	}
	id, err := backend.NewPipeline(PipelineDesc{
		Shader:     shader,
		Topology:   m.topology,
		Blend:      state.Blend,
		DepthTest:  state.DepthTest,
		Stride:     stride,
		Attributes: attrs,
		Label:      "gfx.pipeline",
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
