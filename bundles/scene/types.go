package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
)

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and two Cameras with one ID are an error.
type CameraID = internal.CameraID

// ProjectionKind selects how a camera flattens the world: Perspective,
// Orthographic, or Oblique, which is Orthographic with view-space depth
// sheared into screen-up by the camera's Shear.
type ProjectionKind = internal.ProjectionKind

const (
	Perspective  = internal.Perspective
	Orthographic = internal.Orthographic
	Oblique      = internal.Oblique
)

// PassTag names what a pass is for, and selects which of a Material's tags
// serves it.
type PassTag = internal.PassTag

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward = internal.TagForward

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
type Pass = internal.Pass

// LayerMask selects which cameras see a drawn Entity. A camera draws an Entity
// iff its Layers & the camera's CullMask is non-zero. Zero reads as LayersAll
// on both sides.
type LayerMask = internal.LayerMask

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll = internal.LayersAll

// MaterialTag is one pass tag of a Material Component: a gfx material spelled
// out as the three things it is made of, each laid over what the draw's file
// provides. A zero Shader and a zero State are unset, not values: the draw
// keeps the default scene shader and the file's state. So a tag cannot name the
// zero state, gfx.StateOverlay2D.
//
// It cannot hold a gfx.MaterialDescr, because a descriptor keeps its params as
// a bare slice, which a Component may not hold. The recording System rebuilds
// the descriptor from these fields, in scratch.
type MaterialTag = internal.MaterialTag
