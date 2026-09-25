package internal

import (
	"slices"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/assets"
)

// ensureTexture resolves one texture parameter to the id its binding is set
// from.
//
// The three descriptor cases split here, and only one of them is a cache's: a
// baked texture already carries its id and needs nothing, an inline run was
// baked into one when the frame was recorded, and a path is what the cache is
// for.
func (t *translator) ensureTexture(f *frame, descr TextureDescr) TextureID {
	if id := descr.ID(); id != 0 {
		return id
	}
	if descr.Path() == "" {
		return 0
	}
	return t.textures.Get(f.k, assets.Descr[TextureDescrParams](descr), f.fsys, t.textureUserData(f)).id
}

// textureUserData is what the texture loader is handed on every call. The op queue
// is the translator's own, so a bake or a release the loader emits lands in the
// frame being built exactly where the translator used to put it itself.
func (t *translator) textureUserData(f *frame) textureUserData {
	return textureUserData{backend: f.backend, ops: &t.ops}
}

// ensureShader resolves one material's shader to the module id its draw is
// encoded against, to the label its reports name it by, and to the error that
// module has to say for itself.
func (t *translator) ensureShader(f *frame, descr shader.ShaderDescr) (ShaderID, string, error) {
	cached := t.shaders.Get(
		f.k, assets.Descr[shader.ShaderDescrParams](descr), f.fsys, t.shaderUserData(f, descr.Path()),
	)
	return cached.id, cached.label, cached.report()
}

// shaderUserData is what the shader loader is handed on every call. root is the
// module's path on a load and empty on a free, which is the whole difference
// between the two: a free reads the value, and only a load needs to be told
// what the bytes it was given are called.
func (t *translator) shaderUserData(f *frame, root string) shaderUserData {
	return shaderUserData{t: t, backend: f.backend, root: root}
}

// shaderLayout returns the backend's reflected layout for a shader, cached by id.
func (t *translator) shaderLayout(backend Backend, id ShaderID) shader.ShaderLayout {
	if l, ok := t.layouts[id]; ok {
		return l
	}
	l := backend.ShaderLayout(id)
	t.layouts[id] = l
	return l
}

// ensurePipeline returns the cached pipeline for one (shader, mesh layout,
// state, attachments) combination, building it on a miss.
//
// It returns an error only on the miss that produced it, which is what makes
// report-once-drop-always fall out of the cache that already exists: a failure
// is cached as the zero id, so every frame after the first finds `ok` true,
// returns zero and no error, and the caller drops the draw on the zero id
// exactly as it did before.
func (t *translator) ensurePipeline(
	backend Backend, shaderID ShaderID, label string, m *MeshDescr, state MaterialState, pass PassDescr,
) (PipelineID, error) {
	stride := MeshStride(m)
	layout, ok := VertexLayoutKeyOf(MeshLayout(m))
	if !ok {
		return 0, nil
	}
	// A pipeline declares the formats of the attachments it renders into, so
	// they come from the pass rather than from here. What is not
	// interchangeable is having a colour attachment at all: a depth-only pass
	// has none, and a pipeline that declares a target it will never be given is
	// rejected at setPipeline.
	noColor := pass.Target.IsNone()
	colorFormat := t.targetFormat(backend, pass)
	// Depth is the same: FormatDepth32F is the only depth format in the engine
	// (DepthAuto allocates one, DepthTarget requires one, and the enum holds no
	// other), so what varies is whether the pass has a depth attachment at all.
	noDepth := pass.Depth.IsNone()
	const depthFormat = FormatDepth32F
	k := pipelineKey{
		shader: shaderID, topology: m.Topology(), state: state,
		colorFormat: colorFormat, depthFormat: depthFormat, noColor: noColor, noDepth: noDepth, layout: layout,
		stripIndex: stripIndexKeyOf(m.Topology(), m.IndexWidth()),
	}
	if id, ok := t.pipelines[k]; ok {
		return id, nil
	}
	// The vertex interface is checked before the backend is asked for anything,
	// because no backend checks it: gogpu performs no vertex-interface
	// validation of any kind, the software rasterizer keeps an unsupplied
	// input's zero value, and WebGPU itself fills the components a format does
	// not supply with (0, 0, 0, 1).
	if err := CheckVertexInterface(label, t.shaderLayout(backend, shaderID), MeshLayout(m)); err != nil {
		t.pipelines[k] = 0
		return 0, err
	}
	attrs := make([]VertexAttribute, len(MeshLayout(m)))
	for i := range MeshLayout(m) {
		attrs[i] = VertexAttribute{Offset: VertexAttrOffset(&(MeshLayout(m)[i])), Type: VertexAttrTyp(&(MeshLayout(m)[i])), Location: i}
	}
	id, err := backend.NewPipeline(PipelineDesc{
		Shader:        shaderID,
		Topology:      m.Topology(),
		State:         state,
		ColorFormat:   colorFormat,
		DepthFormat:   depthFormat,
		NoColorTarget: noColor,
		NoDepthTarget: noDepth,
		Stride:        stride,
		Attributes:    attrs,
		IndexWidth:    m.IndexWidth(),
		Label:         "gfx.pipeline",
	})
	if err != nil {
		t.pipelines[k] = 0
		return 0, ErrPipelineFailed{Shader: label, Err: err}
	}
	t.pipelines[k] = id
	return id, nil
}

// targetFormat resolves the colour format a pass's pipelines render into.
//
// A screen pass resolves the sentinel here rather than carrying it into the
// key, so that a screen pass and a pass into a texture of the frame buffer's
// own format share one pipeline instead of building two identical ones - which
// is the common case while every renderable texture in the tree is allocated
// FormatRGBA8Srgb. The backend resolves it either way; only the key can tell
// the difference.
//
// A colourless pass has no format to resolve and is keyed by noColor instead.
//
// A texture target asks the backend, which is where a texture's descriptor
// lives, and which cannot answer on the frame the texture is allocated: the
// bake that creates it is replayed after this frame is translated. That frame
// falls back to the frame buffer's format, which is what every pipeline was
// keyed to before this resolved anything, and it costs nothing - the same
// condition leaves TextureView with no view to return, so the pass is skipped
// and the pipeline keyed here never renders. The frame after keys the target's
// real format, which is a different cache entry and the one that draws.
//
// Falling back rather than refusing the draw is deliberate. ensurePipeline runs
// ahead of the checks that report a draw sampling its own attachment and a
// material missing a storage binding, so a draw dropped here takes their
// diagnostics with it - and it would take them on exactly the frame a caller
// first writes the mistake.
func (t *translator) targetFormat(backend Backend, pass PassDescr) TextureFormat {
	if pass.Target.IsNone() {
		return 0
	}
	if pass.Target.IsScreen() {
		return FormatScreen.Resolve()
	}
	texture, _, _, _ := pass.Target.Texture()
	if format, ok := backend.TextureFormat(texture); ok {
		return format
	}
	return FormatScreen.Resolve()
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

func (t *translator) releaseCachedResource(f *frame, path string) {
	// A path names exactly one texture entry, because TextureWithResource is the
	// only way one is made and it takes no options - so the key a Free names is
	// the key a Get made, and the report that entry filed is forgotten with it.
	t.textures.Free(f.k, assets.Descr[TextureDescrParams](TextureWithResource(path)), t.textureUserData(f))
	// A shader cannot be freed by key, because three things break the probe of
	// one descriptor: a path may root several variants, a path may be an
	// included source of modules rooted elsewhere, and a ShaderWithText shader
	// can include resources, so a text shader is evictable by a path it never
	// names. The decision is over the value, and the value already holds the
	// answer - which is what FreeWhere is, and why gfx builds no reverse
	// path-to-modules index to hold what the include set holds already.
	t.shaders.FreeWhere(f.k, t.shaderUserData(f, ""), func(_ assets.Descr[shader.ShaderDescrParams], value *loadedShader) bool {
		return slices.Contains(value.sources, path)
	})
}

func (t *translator) freeCachedResources(f *frame) {
	// Pipelines and plans go first, and the order is the point. Freeing an entry
	// runs the loader's cascade, which sweeps both maps for the dead ShaderID;
	// left full, that is O(shaders x pipelines) across the FreeAll. Emptied
	// ahead of it, every sweep scans nothing and the pipelines are still handed
	// back exactly once - here, where the shader ids they were keyed on are
	// about to stop meaning anything.
	for _, pipeline := range t.pipelines {
		if pipeline != 0 {
			f.backend.FreePipeline(pipeline)
		}
	}
	clear(t.pipelines)
	clear(t.parameterPlans)

	t.textures.FreeAll(f.k, t.textureUserData(f))
	t.shaders.FreeAll(f.k, t.shaderUserData(f, ""))

	for _, sampler := range t.samplers {
		f.backend.FreeSampler(sampler)
	}
	clear(t.samplers)
	clear(t.layouts)
	clear(t.badIndexLengths)
	clear(t.unsuppliedBuffers)
}
