package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

var (
	unitSphere = m.Sphere{Radius: 1}
	// noSphere is a mesh that carries no baked bounds - a custom layout, or a
	// temporary mesh.
	noSphere = meshRecord{}
	// boxSphere is a mesh with a baked sphere, the unit box's.
	boxSphere = meshRecord{bounds: m.Sphere{Radius: float32(math.Sqrt(3)) / 2}}
)

// Bounds resolve in a fixed order: NeverCull wins, then an explicit non-zero
// sphere, then the mesh's baked sphere, and a draw with none of those is never
// culled rather than culled by a zero sphere at its origin.
func TestBoundsResolveNeverCullThenExplicitThenBakedThenNever(t *testing.T) {
	if _, cull := resolveBounds(true, unitSphere, boxSphere); cull {
		t.Fatal("NeverCull did not short-circuit an explicit sphere and a baked one")
	}
	explicit := m.Sphere{Center: m.Vec3{X: 1}, Radius: 5}
	if sphere, cull := resolveBounds(false, explicit, boxSphere); !cull || sphere != explicit {
		t.Fatalf("an explicit sphere resolved to %v, %v; want the explicit sphere, culled", sphere, cull)
	}
	if sphere, cull := resolveBounds(false, m.Sphere{}, boxSphere); !cull || sphere != boxSphere.bounds {
		t.Fatalf("a zero explicit sphere resolved to %v, %v; want the baked sphere, culled", sphere, cull)
	}
	if _, cull := resolveBounds(false, m.Sphere{}, noSphere); cull {
		t.Fatal("a draw with no bounds anywhere was marked cullable; a zero sphere at the origin would vanish a large mesh the moment its origin left the frustum")
	}
}

// A draw's world sphere scales by the largest axis of its matrix: exact under
// scalar Scale, conservative under a non-uniform Matrix.
func TestWorldRadiusIsTheLocalRadiusTimesTheLargestAxisScale(t *testing.T) {
	scaled := prepareDraw(drawRecord{transform: At(1, 2, 3).WithScale(2)}, boxSphere)
	if !scaled.cullable {
		t.Fatal("a box with a baked sphere is not cullable")
	}
	if scaled.sphere.Center != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("the world sphere is centred at %v, want the draw's position", scaled.sphere.Center)
	}
	if want := 2 * boxSphere.bounds.Radius; !near(scaled.sphere.Radius, want) {
		t.Fatalf("scale 2 gave radius %v, want %v", scaled.sphere.Radius, want)
	}

	stretched := m.Scaling4(1, 5, 2)
	stretchedDraw := prepareDraw(drawRecord{transform: Transform{Matrix: &stretched}}, boxSphere)
	if want := 5 * boxSphere.bounds.Radius; !near(stretchedDraw.sphere.Radius, want) {
		t.Fatalf("a (1,5,2) matrix gave radius %v, want the largest axis' %v", stretchedDraw.sphere.Radius, want)
	}
}

// One cull per distinct frustum: two passes of one camera at the same aspect
// share a result, and a pass at another aspect gets its own.
func TestACameraCullsOncePerDistinctAspect(t *testing.T) {
	draws := []drawRecord{{}, {}}
	prepared := []preparedDraw{
		{sphere: m.Sphere{Center: m.Vec3{Z: -5}, Radius: 1}, cullable: true},
		{sphere: m.Sphere{Center: m.Vec3{Z: 5}, Radius: 1}, cullable: true},
	}
	var c culler
	c.beginCamera()
	view := m.NewMat4()
	frustumAt := func(aspect float32) m.Mat4 { return m.Perspective4(1, aspect, 0.1, 100) }

	first := c.cull(1.5, frustumAt(1.5), view, 0, draws, prepared)
	again := c.cull(1.5, frustumAt(1.5), view, 0, draws, prepared)
	if first != again {
		t.Fatal("the same aspect culled twice")
	}
	other := c.cull(1.0, frustumAt(1.0), view, 0, draws, prepared)
	if other == first || len(c.results) != 2 {
		t.Fatalf("a second aspect produced %d results, want 2 distinct ones", len(c.results))
	}
	result := c.results[first]
	if result.recorded != 2 || result.culled != 1 || result.count != 1 {
		t.Fatalf("cull recorded %d, culled %d, kept %d; want 2, 1, 1", result.recorded, result.culled, result.count)
	}
	survivor := c.survivors[result.first]
	if survivor.draw != 0 {
		t.Fatalf("draw %d survived, want the one in front of the camera", survivor.draw)
	}
	if !near(survivor.depth, 5) {
		t.Fatalf("the survivor's view depth is %v, want 5", survivor.depth)
	}
}

// Culling is what a draw with no bounds is exempt from, and the exemption is
// the documented default rather than an error, so nothing reports it.
func TestANeverCullDrawSurvivesBehindTheCamera(t *testing.T) {
	draws := []drawRecord{{}}
	prepared := []preparedDraw{{sphere: m.Sphere{Center: m.Vec3{Z: 50}}, cullable: false}}
	var c culler
	c.beginCamera()
	result := c.results[c.cull(1, m.Perspective4(1, 1, 0.1, 100), m.NewMat4(), 0, draws, prepared)]
	if result.culled != 0 || result.count != 1 {
		t.Fatalf("a never-cull draw behind the camera was culled: %+v", result)
	}
}

// The layer mask decides what a camera records at all, before the frustum
// decides what it keeps.
func TestACullCountsOnlyTheLayersTheCameraSees(t *testing.T) {
	draws := []drawRecord{{layers: Layer(1)}, {layers: Layer(2)}}
	prepared := []preparedDraw{
		{sphere: m.Sphere{Center: m.Vec3{Z: -5}, Radius: 1}, cullable: true},
		{sphere: m.Sphere{Center: m.Vec3{Z: -5}, Radius: 1}, cullable: true},
	}
	var c culler
	c.beginCamera()
	result := c.results[c.cull(1, m.Perspective4(1, 1, 0.1, 100), m.NewMat4(), Layer(2), draws, prepared)]
	if result.recorded != 1 || result.count != 1 || c.survivors[result.first].draw != 1 {
		t.Fatalf("a camera on layer 2 recorded %d and kept %d, want the one draw on its layer", result.recorded, result.count)
	}
}

func TestASteadyCullAllocatesNothing(t *testing.T) {
	draws := make([]drawRecord, 64)
	prepared := make([]preparedDraw, 64)
	for i := range prepared {
		prepared[i] = preparedDraw{sphere: m.Sphere{Center: m.Vec3{X: float32(i), Z: -5}, Radius: 1}, cullable: true}
	}
	var c culler
	projection := m.Perspective4(1, 1, 0.1, 100)
	c.beginCamera()
	c.cull(1, projection, m.NewMat4(), 0, draws, prepared)
	allocations := testing.AllocsPerRun(50, func() {
		c.beginCamera()
		c.cull(1, projection, m.NewMat4(), 0, draws, prepared)
	})
	if allocations != 0 {
		t.Fatalf("a steady cull allocated %v times", allocations)
	}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
