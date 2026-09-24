package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and two Cameras with one ID are an error.
//
// gfx reserves no ranges, so a camera interleaves with canvas by taking an
// order between two layer values. Cameras conventionally take negative ids
// when canvas draws entirely over them, since an app's canvas layers usually
// start at zero.
type CameraID gfx.Order

// ProjectionKind selects how a camera flattens the world.
//
// Perspective and Orthographic both project along the camera's forward axis, so
// revealing a vertical face always costs ground-plane scale: tilt a camera to
// elevation phi and the ground foreshortens by exactly sin phi. Oblique
// separates the two, which is why it is a kind rather than something an app
// composes from outside - see Camera.Shear.
type ProjectionKind uint8

const (
	Perspective ProjectionKind = iota
	Orthographic
	// Oblique is Orthographic with view-space depth sheared into screen-up by
	// Shear, and at Shear 0 it is exactly Orthographic. It reads Height the
	// same way; what it adds is that the plane the camera sits in renders at
	// true scale while depth is displaced instead.
	Oblique
)

// PassTag names what a pass is for, and selects which of a Material's tags
// serves it. It stays a string so the readable API costs no registration
// handshake.
type PassTag string

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward PassTag = "forward"

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
//
// Target is the gfx handle passed through untouched: the screen sentinel, a
// durable texture from the resource queue, or a frame-local target from
// gfx.OpQueue.TemporaryTarget. A pass with NoTarget() takes its size from an
// explicit depth texture, and one with neither, or with NoTarget() and a
// ClearColor, is reported.
//
// Store ops are inferred, not exposed. Depth is kept iff Depth names a texture;
// colour is always kept.
type Pass struct {
	Tag        PassTag          // zero reads as TagForward
	Target     gfx.TargetDescr  // zero is the screen sentinel; gfx.NoTarget() for depth-only
	Depth      gfx.DepthDescr   // zero is gfx.DepthAuto(), pooled by size and shared
	ClearColor m.Maybe[m.Color] // absent preserves
	ClearDepth m.Maybe[float32] // absent preserves; 1.0 is the useful value
	Order      gfx.Order        // offset from the camera id, not an absolute
}

// LayerMask selects which cameras see a drawn Entity. A camera draws an Entity
// iff its Layers & the camera's CullMask is non-zero.
//
// Zero reads as LayersAll on both sides, so the degenerate frame — one camera,
// one crate, no mask written anywhere — works without the game learning about
// layers at all.
type LayerMask uint32

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll LayerMask = ^LayerMask(0)

// layerCount is the number of distinct layers a LayerMask holds.
const layerCount = 32

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return 1 << (i % layerCount) }
