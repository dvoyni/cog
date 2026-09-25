package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// entry is one instance the recording System will draw, worked out once per
// frame before any camera looks at it: the Batch it belongs to, its world
// matrix, its world-space bounding sphere and whether that sphere is one to
// cull against at all, the layers a camera's mask is tested against, and what
// its instance record says about animation. A Model Entity is one entry per
// primitive of its view, and a Mesh Entity one entry.
//
// Every camera then pays one layer test and one sphere test per entry and
// nothing else.
type entry struct {
	world m.Mat4
	// sphere is the entry's bounds in world space. For an entry that is never
	// culled it is a zero-radius sphere at its origin, kept because the blend
	// sort still needs a point to measure depth to.
	sphere   m.Sphere
	cullable bool
	layers   LayerMask
	// batch is the entry's index in the frame's Batches.
	batch int32
	// anim is the instance record's animation half. Its Offset is the
	// Entity's own sceneAnim block, which is what lets animated Entities
	// sharing a mesh and a material share a Batch.
	anim model.InstanceAnim
}

// resolveBounds picks the local-space sphere an entry is culled by, in a fixed
// order: NeverCull short-circuits, then an explicit non-zero sphere, then the
// mesh's baked one, and an entry with none of those is never culled.
//
// A zero explicit sphere means never-cull rather than a zero-radius sphere at
// the origin, because a large mesh whose origin leaves the frustum would
// otherwise vanish - a silent, camera-angle-dependent bug, the worst kind.
// Drawing too much is a performance problem you can see and profile.
func resolveBounds(neverCull bool, explicit m.Sphere, mesh *model.MeshRecord) (m.Sphere, bool) {
	switch {
	case neverCull:
		return m.Sphere{}, false
	case explicit.Radius != 0:
		return explicit, true
	case mesh.Bounds.Radius != 0:
		return mesh.Bounds, true
	}
	return m.Sphere{}, false
}

// prepareDraw resolves one entry's world sphere from its world matrix. The
// world radius is the local radius times the largest axis scale of the matrix
// - exact under a uniform scale, conservative under a non-uniform one, since a
// sphere under non-uniform scale is not a sphere.
func prepareDraw(
	world m.Mat4, neverCull bool, explicit m.Sphere, mesh *model.MeshRecord,
) (sphere m.Sphere, cullable bool) {
	if local, cull := resolveBounds(neverCull, explicit, mesh); cull {
		return local.Transform(world), true
	}
	return m.Sphere{Center: world.Translation()}, false
}

// survivor is one entry a camera kept, with the view-space depth of its
// sphere's centre already measured, because the view is per camera and the
// blend sort wants the number per pass.
type survivor struct {
	draw  uint32
	depth float32
}

// cullResult is one camera's cull against one frustum: the frustum itself,
// and the survivors' span in the culler's arena.
type cullResult struct {
	aspect  float32
	frustum m.Frustum
	// first and count are the span of survivors this cull kept, in walk
	// order.
	first, count int
}

// culler culls one camera's entries once per distinct frustum. A camera with
// several passes usually has one frustum, because a frustum depends on the
// target's aspect rather than its pixel size; each pass then filters the
// shared survivor list by its own tag. Both slices keep their backing across
// cameras and frames.
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
// if no earlier pass of this camera used the same aspect. The layer mask
// filters first, then the frustum: every survivor's sphere was tested against
// all six planes, far included, which is why a camera's Far is required.
func (c *culler) cull(
	aspect float32, viewProjection, view m.Mat4, cullMask LayerMask, entries []entry,
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
	for i := range entries {
		e := &entries[i]
		if !drawnBy(e.layers, cullMask) {
			continue
		}
		if e.cullable && !result.frustum.ContainsSphere(e.sphere.Center, e.sphere.Radius) {
			continue
		}
		c.survivors = append(c.survivors, survivor{
			draw:  uint32(i),
			depth: -view.TransformPoint(e.sphere.Center).Z,
		})
	}
	result.count = len(c.survivors) - result.first
	c.results = append(c.results, result)
	return len(c.results) - 1
}

// drawnBy reports whether something on layers is drawn by a camera whose
// cull mask is cull. A zero mask on either side reads as every layer, which is
// what makes the zero Component draw and the zero Camera see.
func drawnBy(layers, cull LayerMask) bool {
	return orAll(layers)&orAll(cull) != 0
}

func orAll(l LayerMask) LayerMask {
	if l == 0 {
		return LayersAll
	}
	return l
}
