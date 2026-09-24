package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/libs/m"
)

// ViewProjection is the matrix a camera projects through for a target of the
// given size in pixels, and the identity for a degenerate camera or viewport.
//
// It is published as an output so a caller with many points drops to m.Project
// in a loop over one matrix, and it is the single place the FovY-is-vertical
// rule, the Transform inversion, the ignored camera scale and the 0..1 depth
// convention are encoded.
func ViewProjection(camera CameraDescr, viewport m.Vec2) m.Mat4 {
	return internal.ViewProjection(camera, viewport)
}

// WorldToScreen maps a world point to logical viewport coordinates for a target
// of the given size. X and Y are target pixels from the top-left, and Z is the
// WebGPU 0..1 NDC depth - exactly what ScreenToWorld takes back.
//
// ok is false when the point is at or behind the eye plane, or the camera or
// viewport is degenerate. Off-screen but in front stays true: the coordinate is
// extrapolated past the target edge and is correct there, which is what an
// off-screen indicator arrow needs. Depth outside Near/Far likewise stays true,
// and an orthographic camera never fails the eye-plane test because its clip w
// is 1 everywhere.
//
// Behind the camera never returns a coordinate. Dividing by a negative w yields
// a plausible, mirrored, confidently wrong point, and no NaN sentinel is
// offered either: a bool the compiler makes you look at beats a value that
// silently propagates.
func WorldToScreen(camera CameraDescr, viewport m.Vec2, world m.Vec3) (m.Vec3, bool) {
	return internal.WorldToScreen(camera, viewport, world)
}

// ScreenToWorld maps a screen point back to the world. It takes WorldToScreen's
// output unchanged - X and Y in target pixels from the top-left, Z the 0..1 NDC
// depth - so ScreenToWorld(c, vp, WorldToScreen(c, vp, p)) round-trips.
//
// ok is false for the degenerate case only. A screen point outside the target
// or a depth outside 0..1 is extrapolated, not refused.
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool) {
	return internal.ScreenToWorld(camera, viewport, screen)
}

// ScreenToRay is the ray through a screen pixel, starting on the near plane and
// pointing into the scene. Dir is unit length, so an intersect's t is a world
// distance from that near-plane origin.
//
// ok is false for the degenerate case only. Scene has no list to raycast
// against - draws are frame-local and consumed at flush - so picking is a loop
// over the caller's own entities, calling Bounds or AABB and keeping the
// smallest t.
func ScreenToRay(camera CameraDescr, viewport m.Vec2, screen m.Vec2) (m.Ray, bool) {
	return internal.ScreenToRay(camera, viewport, screen)
}

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return internal.Layer(i) }
