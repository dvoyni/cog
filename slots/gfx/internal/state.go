package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// StateOpaque3D returns the state of opaque geometry, the first of the three
// states the engine's passes are made of: opaque geometry, then transparent
// geometry over it, then 2D on top of everything.
func StateOpaque3D() types.MaterialState { return stateOpaque3D }

// StateTransparent3D returns the state of transparent geometry drawn over
// opaque geometry; see StateOpaque3D.
func StateTransparent3D() types.MaterialState { return stateTransparent3D }

// StateOverlay2D returns the state of 2D drawn on top of everything; see
// StateOpaque3D.
func StateOverlay2D() types.MaterialState { return stateOverlay2D }

var (
	stateOpaque3D      = types.MaterialState{Blend: types.BlendOpaque, DepthCompare: types.CompareLess, DepthWrite: true, Cull: types.CullBack}
	stateTransparent3D = types.MaterialState{Blend: types.BlendAlpha, DepthCompare: types.CompareLess}
	stateOverlay2D     = types.MaterialState{}
)
