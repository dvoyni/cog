package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// MaterialTag is one pass tag of a Material Component: a shader, a state and
// params, each laid over what the draw's file provides. A zero Shader and a
// zero State are unset, not values: the draw keeps the default scene shader and
// the file's state. So a tag cannot name the zero state, gfx.StateOverlay2D.
//
// Its params are an m.List rather than a bare slice, which a Component may not
// hold. The load System resolves the tag into a set of draw params, cached by
// the material's key.
type MaterialTag struct {
	// Tag is the pass this entry serves; zero reads as TagForward.
	Tag PassTag
	// Shader is resolved under the draw's SCENE_SKIN and SCENE_MORPH, which
	// the draw's geometry decides; zero is the default scene shader.
	Shader gfx.ShaderDescr
	// State is the pipeline state; zero is the file's.
	State gfx.DrawState
	// Params are overlaid by name on the file's and the default scene
	// shader's.
	Params m.List[gfx.ShaderParameterDescr]
}
