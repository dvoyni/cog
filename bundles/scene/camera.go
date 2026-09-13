package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and registering one twice is an error.
//
// gfx reserves no ranges, so a camera interleaves with canvas by taking an
// order between two layer values. Scene cameras conventionally take negative
// ids when canvas draws entirely over them, since an app's canvas layers
// usually start at zero. Two recorders emitting passes at an equal Order is an app-level bug
// that promises nothing: gfx breaks the tie by declaration sequence, and canvas
// and scene flush from separate subscriptions with no order defined between
// them, so which one lands first is not a property the app can rely on.
type CameraID = internal.CameraID

// ProjectionKind selects how a camera flattens the world.
//
// Perspective and Orthographic both project along the camera's forward axis, so
// revealing a vertical face always costs ground-plane scale: tilt a camera to
// elevation phi and the ground foreshortens by exactly sin phi. Oblique
// separates the two, which is why it is a kind rather than something an app
// composes from outside - see Shear.
type ProjectionKind = internal.ProjectionKind

const (
	Perspective  = internal.Perspective
	Orthographic = internal.Orthographic
	// Oblique is Orthographic with view-space depth sheared into screen-up by
	// Shear, and at Shear 0 it is exactly Orthographic. It reads Height the
	// same way; what it adds is that the plane the camera sits in renders at
	// true scale while depth is displaced instead.
	Oblique = internal.Oblique
)

// PassTag names what a pass is for, and selects which of a material's gfx
// materials serves it. It stays a string so the readable API costs no
// registration handshake; scene interns it once per pass, not once per draw.
type PassTag = internal.PassTag

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward = internal.TagForward

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
//
// Target is the gfx handle passed through untouched: the screen sentinel, a
// durable texture from the resource queue, or a frame-local target from
// gfx.OpQueue.TemporaryTarget. Scene keeps no name registry over it and offers
// no allocator of its own, because minting a texture takes the gfx queue, which
// a scene recorder does not hold; an app that renders a camera into a temporary
// target locks both queues and hands the target across. A pass with NoTarget()
// takes its size from an explicit depth texture, and one with neither, or with
// NoTarget() and a ClearColor, is reported.
//
// Depth sharing with canvas: canvas declares DepthAuto on every pass it emits,
// so a screen-sized camera pass and canvas draw against the same pooled depth
// texture. That is harmless today, because no built-in canvas material tests
// depth, but a caller-supplied depth-testing canvas material will interact with
// the 3D depth left there. An app wanting isolation gets it by not ordering the
// camera adjacent to canvas, or by giving the pass a depth texture of its own.
//
// Store ops are inferred, not exposed. Depth is kept iff Depth names a texture;
// colour is always kept, and LoadDiscard on colour is not offered, since it
// only pays for a pass that provably covers its whole target.
type Pass = internal.Pass

// CameraDescr is everything a camera is. Clears live on its passes, not here:
// carrying them on both, with the camera's ignored once Passes is non-empty, is
// a silent-override rule that produces bug reports.
type CameraDescr = internal.CameraDescr

// LayerMask selects which cameras see a recorded item. A camera draws an item
// iff layers & CullMask is non-zero.
//
// Zero reads as LayersAll on both sides, so the degenerate frame — one camera,
// one box, no mask written anywhere — works without the caller learning about
// layers at all.
type LayerMask = internal.LayerMask

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll = internal.LayersAll

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return internal.Layer(i) }

// LightKind is which of the two punctual lights a LightDescr describes.
// PointLight and SpotLight set it themselves; it is exposed so an Op can be
// read back.
type LightKind = internal.LightKind

const (
	LightPoint = internal.LightPoint
	LightSpot  = internal.LightSpot
)

// LightDescr is one punctual light, point or spot, over the one struct: call
// sites stay explicit through PointLight and SpotLight, a hand-written point
// light leaves the cone fields zero, and the per-camera light buffer is
// homogeneous without scene converting between two structs.
//
// Every zero is a default. Intensity zero means 1. Range zero means infinite,
// glTF's own default - a forgotten Range yields a light that reaches too far,
// which you see immediately, rather than a silently skipped light. OuterCone
// zero means pi/4, glTF's default; InnerCone zero is a real value, falloff
// from the axis.
type LightDescr = internal.LightDescr
