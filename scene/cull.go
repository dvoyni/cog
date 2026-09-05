package scene

import "github.com/dvoyni/cog/m"

// preparedDraw is what the flush works out about one recorded draw once per
// frame, before any camera looks at it: its world matrix, its world-space
// bounding sphere, and whether that sphere is one to cull against at all. Every
// camera then pays one sphere test per draw and nothing else.
type preparedDraw struct {
	world m.Mat4
	// sphere is the draw's bounds in world space. For a draw that is never
	// culled it is a zero-radius sphere at the draw's origin, kept because the
	// blend sort still needs a point to measure depth to.
	sphere   m.Sphere
	cullable bool
	mesh     MeshRef
	// interned is the draw's material's index in the frame's material table.
	interned int32
}

// resolveBounds picks the local-space sphere a draw is culled by, in a fixed
// order: NeverCull short-circuits, then an explicit non-zero sphere, then the
// mesh's baked one, and a draw with none of those is never culled.
//
// A zero explicit sphere means never-cull rather than a zero-radius sphere at
// the origin, because a large mesh whose origin leaves the frustum would
// otherwise vanish - a silent, camera-angle-dependent bug, the worst kind.
// Drawing too much is a performance problem you can see and profile. Never-cull
// is never reported for the same reason: it is the documented default, not an
// error.
func resolveBounds(neverCull bool, explicit m.Sphere, mesh meshRecord) (m.Sphere, bool) {
	switch {
	case neverCull:
		return m.Sphere{}, false
	case explicit.Radius != 0:
		return explicit, true
	case mesh.bounds.Radius != 0:
		return mesh.bounds, true
	}
	return m.Sphere{}, false
}

// prepareDraw resolves one recorded draw's world matrix and world sphere. The
// world radius is the local radius times the largest axis scale of the matrix -
// exact under scalar Scale, conservative under the Matrix escape hatch, since a
// sphere under non-uniform scale is not a sphere.
func prepareDraw(record drawRecord, mesh meshRecord) preparedDraw {
	world := record.transform.Mat4()
	prepared := preparedDraw{world: world}
	if local, cull := resolveBounds(record.neverCull, record.bounds, mesh); cull {
		prepared.sphere = local.Transform(world)
		prepared.cullable = true
	} else {
		prepared.sphere.Center = world.Translation()
	}
	return prepared
}

// survivor is one draw a camera kept, with the view-space depth of its sphere's
// centre already measured, because the view is per camera and the blend sort
// wants the number per pass.
type survivor struct {
	draw  uint32
	depth float32
}

// cullResult is one camera's cull against one frustum: the frustum itself,
// published so a test can assert a specific sphere was rejected by it, the
// survivors' span in the culler's arena, and the counts a PassView reports.
type cullResult struct {
	aspect  float32
	frustum m.Frustum
	// first and count are the span of survivors this cull kept, in recording
	// order.
	first, count int
	// recorded is how many draws the camera's layer mask selected, and culled
	// how many of those the frustum rejected.
	recorded, culled int
}

// culler culls one camera's draws once per distinct frustum. A camera with
// several passes usually has one frustum, because a frustum depends on the
// target's aspect rather than its pixel size; each pass then filters the shared
// survivor list by its own tag. Both slices keep their backing across cameras
// and frames.
type culler struct {
	results   []cullResult
	survivors []survivor
}

// beginCamera forgets the previous camera's results.
func (c *culler) beginCamera() {
	c.results = c.results[:0]
	c.survivors = c.survivors[:0]
}

// cull returns the index of the camera's result for one aspect, culling only
// if no earlier pass of this camera used the same aspect. Every survivor's
// sphere was tested against all six planes of the frustum, far included, which
// is why a camera's Far is required.
func (c *culler) cull(
	aspect float32, viewProjection, view m.Mat4, cullMask LayerMask,
	draws []drawRecord, prepared []preparedDraw,
) int {
	for i := range c.results {
		if c.results[i].aspect == aspect {
			return i
		}
	}
	result := cullResult{
		aspect:  aspect,
		frustum: m.FrustumFromMat4(viewProjection),
		first:   len(c.survivors),
	}
	for i := range draws {
		if !draws[i].layers.drawnBy(cullMask) {
			continue
		}
		result.recorded++
		sphere := &prepared[i].sphere
		if prepared[i].cullable && !result.frustum.ContainsSphere(sphere.Center, sphere.Radius) {
			result.culled++
			continue
		}
		c.survivors = append(c.survivors, survivor{
			draw:  uint32(i),
			depth: -view.TransformPoint(sphere.Center).Z,
		})
	}
	result.count = len(c.survivors) - result.first
	c.results = append(c.results, result)
	return len(c.results) - 1
}
