package m

// The projection maths a renderer's camera is built from, over plain
// parameters. A renderer owns its camera type and its projection kinds; it
// resolves them to these arguments and calls here, so every renderer projects,
// shades and maps coordinates by the same arithmetic.
//
// Screen is logical viewport coordinates, origin top-left, Y down. WebGPU NDC
// is Y-up and origin-centre, so WorldToScreen, ScreenToWorld and ScreenToRay
// flip Y:
//
//	x = (ndc.x + 1) * 0.5 * viewport.X
//	y = (1 - ndc.y) * 0.5 * viewport.Y
//
// Every one of them takes the target's size in pixels, and a viewport with a
// zero or negative side answers ok = false.

// ErrProjectionDegenerate reports camera parameters that build no projection.
// Reason names the parameter at fault; a renderer re-reports it under its own
// camera's identity.
type ErrProjectionDegenerate struct {
	Reason string
}

func (e ErrProjectionDegenerate) Error() string {
	return "m: no projection: " + e.Reason
}

// Projection builds a camera's projection matrix at one target's aspect.
//
// A perspective camera reads fovY, the literal vertical field of view in
// radians, and ignores height and shear. A parallel camera reads height, the
// world units across the target's height, and shear, how far one world unit of
// view-space depth rides up the screen: 0 is orthographic, anything else
// oblique. It ignores fovY. Horizontal derives from the aspect either way, so a
// wider target shows more horizontally and a narrower one crops the sides.
//
// The error is an ErrProjectionDegenerate: near at or past far, or the one
// extent the kind reads zero or negative. Nothing about a shear is degenerate -
// 0 is the continuum's endpoint, which a shear animated up from rest must pass
// through, a negative one is a mirror, and a large one is only a useless
// elevation.
func Projection(perspective bool, fovY, height, shear, near, far, aspect float32) (Mat4, error) {
	if near >= far {
		return Mat4{}, ErrProjectionDegenerate{Reason: "Near is at or past Far"}
	}
	if !perspective {
		if height <= 0 {
			return Mat4{}, ErrProjectionDegenerate{Reason: "Height is zero or negative"}
		}
		half := height / 2
		return Oblique4(-half*aspect, half*aspect, -half, half, near, far, shear), nil
	}
	if fovY <= 0 {
		return Mat4{}, ErrProjectionDegenerate{Reason: "FovY is zero or negative"}
	}
	return Perspective4(fovY, aspect, near, far), nil
}

// CameraView inverts a camera's transform into a view matrix, ignoring Scale.
// Scaling a view matrix scales the whole world instead, and the field cannot be
// avoided at the call site because its zero already means one.
func CameraView(camera Transform) (Mat4, bool) {
	return cameraBasis(camera).InverseAffine()
}

// cameraBasis is the world matrix a camera is read through: its own, with the
// scale dropped, because a scaled camera scales the world instead.
//
// It exists so CameraView and ViewDirection cannot disagree about which matrix
// the camera is. They resolve the same rotation from it, one inverted and one
// not, and a scale applied to one but not the other would tilt every
// view-dependent shading term against the geometry it shades.
func cameraBasis(camera Transform) Mat4 {
	camera.Scale = Vec3{}
	return camera.Mat4()
}

// ViewDirection is the direction a camera's viewer looks from, packed as a
// shader reads it: xyz the constant world direction from a surface towards the
// viewer, and w a mix selector - 1 when that constant is the answer, 0 when the
// shader must difference against the camera position per fragment instead.
// shear is the one Projection applied, so 0 for an orthographic camera.
//
// Only a perspective camera has a real eye, and only there is the view vector
// radial. A parallel camera projects along parallel rays, so its view direction
// is one vector for the whole frame. Getting this from the camera's translation
// is wrong for both parallel kinds: an orthographic camera has no eye point,
// and an oblique one looks one way while its viewer sees another, so every
// view-dependent term lights vertical faces as if edge-on and floors as if
// head-on.
//
// The ray is the direction that leaves both screen coordinates unchanged. The
// projection sends (x, y, z) to (x, y + shear*z), so a direction moves nothing
// when dx is 0 and dy + shear*dz is 0: (0, -shear, 1) in view space, which at
// shear 0 is the camera's own +Z and so serves orthographic by the same line.
func ViewDirection(camera Transform, perspective bool, shear float32) Vec4 {
	if perspective {
		return Vec4{}
	}
	// The basis rather than the transform's own matrix: CameraView drops a TRS
	// camera's scale, so reading the scaled matrix here would put the direction
	// and the view matrix into disagreement. TransformDirection is the right
	// operation either way - a projection ray is a direction, not a normal, so
	// no inverse-transpose question arises.
	ray := cameraBasis(camera).TransformDirection(Vec3{Y: -shear, Z: 1}).Normalize()
	return Vec4{X: ray.X, Y: ray.Y, Z: ray.Z, W: 1}
}

// WorldToScreen maps a world point through a view-projection to logical
// viewport coordinates for a target of the given size. X and Y are target
// pixels from the top-left, and Z is the WebGPU 0..1 NDC depth - exactly what
// ScreenToWorld takes back.
//
// ok is false when the point is at or behind the eye plane or the viewport is
// degenerate. Off-screen but in front stays true: the coordinate is
// extrapolated past the target edge and is correct there.
func WorldToScreen(viewProjection Mat4, viewport Vec2, world Vec3) (Vec3, bool) {
	if viewport.X <= 0 || viewport.Y <= 0 {
		return Vec3{}, false
	}
	ndc, ok := Project(viewProjection, world)
	if !ok {
		return Vec3{}, false
	}
	return Vec3{
		X: (ndc.X + 1) * 0.5 * viewport.X,
		Y: (1 - ndc.Y) * 0.5 * viewport.Y,
		Z: ndc.Z,
	}, true
}

// ScreenToWorld maps a screen point back to the world through a
// view-projection. It takes WorldToScreen's output unchanged, so the two
// round-trip.
//
// ok is false when the viewport is degenerate or the view-projection has no
// inverse. A screen point outside the target or a depth outside 0..1 is
// extrapolated, not refused. A perspective view-projection is not affine, so
// the inverse is the general Mat4.Inverse, which allocates.
func ScreenToWorld(viewProjection Mat4, viewport Vec2, screen Vec3) (Vec3, bool) {
	inverse, ok := inverseViewProjection(viewProjection, viewport)
	if !ok {
		return Vec3{}, false
	}
	return Unproject(inverse, screenToNDC(viewport, screen)), true
}

// ScreenToRay is the ray through a screen pixel, starting on the near plane and
// pointing into the scene. Dir is unit length, so an intersect's t is a world
// distance from that near-plane origin. ok is false as for ScreenToWorld.
func ScreenToRay(viewProjection Mat4, viewport Vec2, screen Vec2) (Ray, bool) {
	inverse, ok := inverseViewProjection(viewProjection, viewport)
	if !ok {
		return Ray{}, false
	}
	origin := Unproject(inverse, screenToNDC(viewport, Vec3{X: screen.X, Y: screen.Y}))
	into := Unproject(inverse, screenToNDC(viewport, Vec3{X: screen.X, Y: screen.Y, Z: 1}))
	return NewRay(origin, into.Sub(origin)), true
}

// inverseViewProjection refuses a degenerate viewport before inverting, so the
// two screen-to-world functions share one gate.
func inverseViewProjection(viewProjection Mat4, viewport Vec2) (Mat4, bool) {
	if viewport.X <= 0 || viewport.Y <= 0 {
		return Mat4{}, false
	}
	return viewProjection.Inverse()
}

// screenToNDC undoes the Y flip and the pixel scale. Z passes through: clip
// depth is already the 0..1 convention on both sides.
func screenToNDC(viewport Vec2, screen Vec3) Vec3 {
	return Vec3{
		X: screen.X/viewport.X*2 - 1,
		Y: 1 - screen.Y/viewport.Y*2,
		Z: screen.Z,
	}
}
