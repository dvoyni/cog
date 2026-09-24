package internal

import (
	"slices"

	"github.com/dvoyni/cog/slots/gfx"
)

// SceneShaderDescr is the default scene shader: the shader a draw uses when
// nothing it names sets one, with params of its own. Its zero value is the
// bundled PBR with no params.
//
// Source is resolved under the draw's variant defines like any other shader,
// so it declares group 2 behind SCENE_SKIN and SCENE_MORPH as the bundled one
// does - which is what including VertexStagePath gives it.
//
// Params are overlaid under a draw's own, over the file's: a default shader
// carries the bindings it needs - a scene-wide texture, say - with nothing set
// per Entity, and an Entity's same-named param still wins. They are bound on
// every draw whatever shader is in effect, and a shader declaring none of
// their names never reads them.
type SceneShaderDescr struct {
	Source gfx.ShaderDescr
	Params []gfx.ParameterDescr
}

// SetDefaultSceneShader makes descr the default scene shader from the next
// frame on; the zero descriptor restores the bundled PBR. The params are copied
// at the call, so the caller's slice is its own again when this returns.
//
// It is a call rather than configuration because its params are typically
// baked resources, which exist only once the backend does. Nothing is rebuilt:
// a renderer resolves its materials from their ingredients every frame, so a
// model loaded before the call draws under the new default on the next one.
func (la LookupAccess) SetDefaultSceneShader(descr SceneShaderDescr) {
	if !la.Valid() {
		return
	}
	descr.Params = slices.Clone(descr.Params)
	la.lookup.defaultShader = descr
}

// DefaultSceneShader reports the default scene shader, zero while none is set.
// The params alias the Lookup's copy and must not be written to.
func (ra LookupReadAccess) DefaultSceneShader() SceneShaderDescr {
	if !ra.Valid() {
		return SceneShaderDescr{}
	}
	return ra.lookup.defaultShader
}
