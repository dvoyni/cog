package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and registering one twice is an error.
//
// gfx reserves no ranges, so a camera interleaves with canvas by taking an
// order between two layer values. Scene cameras conventionally take negative
// ids when canvas draws entirely over them.
type CameraID gfx.Order

// ProjectionKind selects how a camera flattens the world.
type ProjectionKind uint8

const (
	Perspective ProjectionKind = iota
	Orthographic
)

// PassTag names what a pass is for, and selects which of a material's gfx
// materials serves it. It stays a string so the readable API costs no
// registration handshake; scene interns it once per pass, not once per draw.
type PassTag string

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward PassTag = "forward"

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
type Pass struct {
	Tag        PassTag         // zero reads as TagForward
	Target     gfx.TargetDescr // zero is the screen sentinel; gfx.NoTarget() for depth-only
	Depth      gfx.DepthDescr  // zero is gfx.DepthAuto()
	ClearColor *m.Color        // nil preserves
	ClearDepth *float32        // nil preserves; 1.0 is the useful value
	Order      gfx.Order       // offset from the camera id, not an absolute
}

// CameraDescr is everything a camera is. Clears live on its passes, not here:
// carrying them on both, with the camera's ignored once Passes is non-empty, is
// a silent-override rule that produces bug reports.
type CameraDescr struct {
	Transform  Transform // the camera as a positioned object; scene inverts it
	Projection ProjectionKind
	FovY       float32 // Perspective: the literal vertical field of view, radians
	Height     float32 // Orthographic: world units across the target's height
	Near, Far  float32 // both required; a zero in either is a reported error

	CullMask LayerMask // zero reads as LayersAll

	SunDirection m.Vec3 // direction of travel; zero means no sun; scene normalises
	SunColor     m.Color
	SunIntensity float32 // zero means 1

	AmbientSky       m.Color
	AmbientGround    m.Color
	AmbientIntensity float32 // zero means 1

	Passes []Pass // empty means one default pass
}

// depthClearFar is the depth a pass clears to. Depth is conventional — near
// maps to 0, far to 1, compare Less — so the useful clear is the far plane. The
// naive ClearDepth: &zero clears to the near plane and hides the whole scene.
var depthClearFar float32 = 1

// defaultPass is the single pass a camera with no declared passes emits: the
// forward tag, the screen, at the camera's own order, colour preserved and
// depth cleared to the far plane.
//
// The asymmetry is deliberate. Defaulting to a colour clear would let a second
// camera silently erase the first. But a DepthAuto pass shares its pooled depth
// texture with every other same-size DepthAuto pass in the frame, so it must
// clear depth or render against whatever the previous pass left there.
func defaultPass() Pass {
	return Pass{Tag: TagForward, ClearDepth: &depthClearFar}
}

// tag reads an unwritten tag as the forward pass.
func (p Pass) tag() PassTag {
	if p.Tag == "" {
		return TagForward
	}
	return p.Tag
}
