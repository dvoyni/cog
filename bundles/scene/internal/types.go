package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// MaterialTag is one pass tag of a Material Component: a gfx material spelled
// out as the three things it is made of, each laid over what the draw's file
// provides. A zero Shader and a zero State are unset, not values: the draw
// keeps the default scene shader and the file's state. So a tag cannot name the
// zero state, gfx.StateOverlay2D.
//
// It cannot hold a gfx.MaterialDescr, because a descriptor keeps its params as
// a bare slice, which a Component may not hold. The recording System rebuilds
// the descriptor from these fields, in scratch.
type MaterialTag struct {
	// Tag is the pass this entry serves; zero reads as TagForward.
	Tag PassTag
	// Shader is resolved under the draw's SCENE_SKIN and SCENE_MORPH, which
	// the draw's geometry decides; zero is the default scene shader.
	Shader gfx.ShaderDescr
	// State is the pipeline state; zero is the file's.
	State gfx.MaterialState
	// Params are overlaid by name on the file's and the default scene
	// shader's.
	Params m.List[gfx.ParameterDescr]
}
