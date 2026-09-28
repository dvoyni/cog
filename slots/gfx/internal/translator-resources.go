package internal

import (
	"slices"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

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
func (t *translator) ensureTexture(f *frame, descr types.TextureDescr) types.TextureID {
	if id := descr.Params.ID; id != 0 {
		return id
	}
	if descr.Name == "" {
		return 0
	}
	return t.textures.Get(f.k, assets.Descr[types.TextureDescrParams](descr), f.fsys, t.textureUserData(f)).id
}

// textureUserData is what the texture loader is handed on every call. The op queue
// is the translator's own, so a bake or a release the loader emits lands in the
// frame being built exactly where the translator used to put it itself.
func (t *translator) textureUserData(f *frame) textureUserData {
	return textureUserData{backend: f.backend, ops: &t.ops}
}

// createShader creates the module of an uploaded program under the id
// ResourceQueue.NewShader reserved for it. The layout is the program's own, so
// the backend is never asked to reflect again, and it is measured against the
// web floor here, once, because the upload is replayed once.
func (t *translator) createShader(backend Backend, id types.ShaderID, program shader.ShaderProgram) error {
	desc := shader.ProgramDesc(program)
	if err := backend.CreateShader(id, desc); err != nil {
		// Nothing the backend said is rewritten and no line number is parsed out
		// of its message: the segment table rides beside it and the reader
		// subtracts.
		return shader.CompileError(desc.Label, err, shader.ProgramSourceMap(program))
	}
	if diagnostic := checkWebLimits(desc.Label, shader.ProgramLayout(program), backend.Limits()); diagnostic != nil && t.diagnostic == nil {
		t.diagnostic = diagnostic
	}
	return nil
}

// releaseShader frees a shader's module and the pipelines keyed on its id.
func (t *translator) releaseShader(backend Backend, id types.ShaderID) {
	for key, pipeline := range t.pipelines {
		if key.shader != id {
			continue
		}
		// A zero entry is the marker for a pipeline that failed to build, not
		// a resource: there is nothing to hand back.
		if pipeline != 0 {
			backend.FreePipeline(pipeline)
		}
		delete(t.pipelines, key)
	}
	backend.FreeShader(id)
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
	backend Backend, shaderID types.ShaderID, program shader.ShaderProgram, m *types.MeshDescr, state types.DrawState, pass types.PassDescr,
) (types.PipelineID, error) {
	layout, ok := VertexLayoutKeyOf(m.Layout)
	if !ok {
		return 0, nil
	}
	// A pipeline declares the formats of the attachments it renders into, so
	// they come from the pass rather than from here. What is not
	// interchangeable is having a colour attachment at all: a depth-only pass
	// has none, and a pipeline that declares a target it will never be given is
	// rejected at setPipeline.
	noColor := pass.Target.Kind == types.TargetNone
	colorFormat := t.targetFormat(backend, pass)
	// Depth is the same: FormatDepth32F is the only depth format in the engine
	// (DepthDescrAuto allocates one, DepthDescrTarget requires one, and the enum holds no
	// other), so what varies is whether the pass has a depth attachment at all.
	noDepth := pass.Depth.Kind == types.DepthKindNone
	const depthFormat = types.FormatDepth32F
	k := pipelineKey{
		shader: shaderID, topology: m.Topology, state: state,
		colorFormat: colorFormat, depthFormat: depthFormat, noColor: noColor, noDepth: noDepth, layout: layout,
		stripIndex: stripIndexKeyOf(m.Topology, m.IndexWidth),
	}
	if id, ok := t.pipelines[k]; ok {
		return id, nil
	}
	// The vertex interface is checked before the backend is asked for anything,
	// because no backend checks it: gogpu performs no vertex-interface
	// validation of any kind, the software rasterizer keeps an unsupplied
	// input's zero value, and WebGPU itself fills the components a format does
	// not supply with (0, 0, 0, 1).
	if err := CheckVertexInterface(program.Label(), shader.ProgramLayout(program), m.Layout); err != nil {
		t.pipelines[k] = 0
		return 0, err
	}
	// The layout lives in the frame's arena, and a backend may keep the
	// descriptor it was built from.
	attrs := slices.Clone(m.Layout)
	id, err := backend.NewPipeline(PipelineDesc{
		Shader:        shaderID,
		Topology:      m.Topology,
		State:         state,
		ColorFormat:   colorFormat,
		DepthFormat:   depthFormat,
		NoColorTarget: noColor,
		NoDepthTarget: noDepth,
		Stride:        m.Stride,
		Attributes:    attrs,
		IndexWidth:    m.IndexWidth,
		Label:         "gfx.pipeline",
	})
	if err != nil {
		t.pipelines[k] = 0
		return 0, ErrPipelineFailed{Shader: program.Label(), Err: err}
	}
	t.pipelines[k] = id
	return id, nil
}

// targetFormat resolves the colour format a pass's pipelines render into.
//
// A screen pass is keyed on the frame buffer's format itself, so that it and a
// pass into a texture of that format share one pipeline instead of building two
// identical ones - which is the common case while every renderable texture in
// the tree is allocated FormatRGBA8Srgb.
//
// A colourless pass has no format to resolve and is keyed by noColor instead.
//
// A texture target asks the backend, which is where a texture's descriptor
// lives, and which cannot answer on the frame the texture is allocated: the
// allocation is replayed after this frame is translated. That frame's
// allocation passed through the translator, though, so its format is read
// from there, and the pass renders on its first frame with the pipeline that
// matches its attachment. A texture neither knows falls back to the frame
// buffer's format.
//
// Falling back rather than refusing the draw is deliberate. ensurePipeline runs
// ahead of the checks that report a draw sampling its own attachment and a
// material missing a storage binding, so a draw dropped here takes their
// diagnostics with it - and it would take them on exactly the frame a caller
// first writes the mistake.
func (t *translator) targetFormat(backend Backend, pass types.PassDescr) types.TextureFormat {
	if pass.Target.Kind == types.TargetNone {
		return 0
	}
	if pass.Target.Kind == types.TargetScreen {
		return types.FrameBufferFormat
	}
	texture := pass.Target.Texture
	if format, ok := backend.TextureFormat(texture); ok {
		return format
	}
	if format, ok := t.allocated[texture]; ok {
		return format
	}
	return types.FrameBufferFormat
}

func (t *translator) ensureSampler(backend Backend, desc types.SamplerDesc) types.SamplerID {
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
	// Shaders are not cached here: each is the caller's, created and released
	// through the ResourceQueue.
	t.textures.Free(f.k, assets.Descr[types.TextureDescrParams](types.TextureWithResource(path)), t.textureUserData(f))
}

func (t *translator) freeCachedResources(f *frame) {
	// Pipelines are rebuilt on the next draw that needs one; the shaders they
	// were keyed on are the caller's, and stay.
	for _, pipeline := range t.pipelines {
		if pipeline != 0 {
			f.backend.FreePipeline(pipeline)
		}
	}
	clear(t.pipelines)

	t.textures.FreeAll(f.k, t.textureUserData(f))

	for _, sampler := range t.samplers {
		f.backend.FreeSampler(sampler)
	}
	clear(t.samplers)
	clear(t.badIndexLengths)
	clear(t.unsuppliedBuffers)
}
