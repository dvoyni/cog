package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

var testLineColor = m.NewColorSrgb(1, 0.8, 0.2, 1)

// Every debug call is one Op reporting the call as made, whatever it flushes
// to, and Ops keeps the recording order behind the cameras.
func TestEveryDebugCallIsOneOpReportingItsArguments(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Sphere(scene.Layer(1), m.Vec3{X: 1, Y: 2, Z: 3}, 0.5, testBoxColor)
		q.Plane(0, m.Vec3{Y: -1}, m.Vec2{X: 4, Y: 6}, testBoxColor)
		q.Line3D(0, m.Vec3{}, m.Vec3{X: 2}, 0.1, testLineColor)
		q.WireBox(0, m.Vec3{Z: 1}, m.Vec3{X: 1, Y: 2, Z: 3}, 0.05, testLineColor)
		q.Box(0, m.At(7, 0, 0), testBoxColor)
	})
	h.frame()

	ops := h.ops()
	if len(ops) != 6 {
		t.Fatalf("recorded %d ops, want a camera and five calls", len(ops))
	}
	sphere, plane, line, wire, box := ops[1], ops[2], ops[3], ops[4], ops[5]
	if sphere.Kind != scene.OpSphere || sphere.Layers != scene.Layer(1) || sphere.Center != (m.Vec3{X: 1, Y: 2, Z: 3}) || sphere.Radius != 0.5 {
		t.Fatalf("the sphere op is %+v", sphere)
	}
	if plane.Kind != scene.OpPlane || plane.Center != (m.Vec3{Y: -1}) || plane.Size != (m.Vec3{X: 4, Z: 6}) {
		t.Fatalf("the plane op is %+v", plane)
	}
	if line.Kind != scene.OpLine3D || line.End != (m.Vec3{X: 2}) || line.Thickness != 0.1 || line.Color != testLineColor {
		t.Fatalf("the line op is %+v", line)
	}
	if wire.Kind != scene.OpWireBox || wire.Center != (m.Vec3{Z: 1}) || wire.Size != (m.Vec3{X: 1, Y: 2, Z: 3}) || wire.Thickness != 0.05 {
		t.Fatalf("the wire box op is %+v", wire)
	}
	if box.Kind != scene.OpBox || box.Transform.Position != (m.Vec3{X: 7}) {
		t.Fatalf("the box op is %+v", box)
	}
	h.inspect(func(q *scene.OpQueue) {
		if q.OpCount() != 0 {
			t.Fatalf("OpCount reads %d after the flush, want the recording in progress, which is empty", q.OpCount())
		}
	})
}

// A wire box is one call and twelve draws: each edge is its own instance,
// culled on its own.
func TestAWireBoxFlushesToTwelveEdges(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.WireBox(0, m.Vec3{}, m.Vec3{X: 1, Y: 1, Z: 1}, 0.05, testLineColor)
	})
	h.frame()

	pass := h.passes()[0]
	if pass.Recorded != 12 || pass.Instances != 12 {
		t.Fatalf("recorded %d, packed %d; want 12 edges", pass.Recorded, pass.Instances)
	}
	if len(h.backend.draws) != 12 {
		t.Fatalf("the backend received %d draws, want 12 edges", len(h.backend.draws))
	}
}

// A shape of no size records its call and draws nothing, rather than packing
// a collapsed instance whose basis the shader would try to invert.
func TestAShapeOfNoSizeDrawsNothing(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Sphere(0, m.Vec3{}, 0, testBoxColor)
		q.Plane(0, m.Vec3{}, m.Vec2{X: 1}, testBoxColor)
		q.Line3D(0, m.Vec3{X: 1}, m.Vec3{X: 1}, 0.1, testLineColor)
		q.Line3D(0, m.Vec3{}, m.Vec3{X: 1}, 0, testLineColor)
		q.WireBox(0, m.Vec3{}, m.Vec3{}, 0.1, testLineColor)
	})
	h.frame()

	if ops := h.ops(); len(ops) != 6 {
		t.Fatalf("recorded %d ops, want the camera and all five calls", len(ops))
	}
	if pass := h.passes()[0]; pass.Recorded != 0 {
		t.Fatalf("recorded %d draws, want none", pass.Recorded)
	}
}

// The unit sphere and plane bake once each, the first time something draws
// them, and a frame that draws only boxes never bakes them at all.
func TestTheUnitSphereAndPlaneBakeLazilyAndOnce(t *testing.T) {
	shapes := 0
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, m.At(0, 0, 0), testBoxColor)
		for range shapes {
			q.Sphere(0, m.Vec3{}, 1, testBoxColor)
			q.Plane(0, m.Vec3{}, m.Vec2{X: 1, Y: 1}, testBoxColor)
		}
	})
	h.frame()
	// Five arenas and the unit box's two buffers. A box declares no group 2, so
	// there is nothing else for it to bake.
	if h.backend.bakes != 7 {
		t.Fatalf("a boxes-only first frame uploaded %d buffers, want 7", h.backend.bakes)
	}

	shapes = 3
	h.backend.bakes = 0
	h.frame()
	// Five arenas, plus two buffers each for the sphere and the plane, once,
	// however many of them the frame draws.
	if h.backend.bakes != 9 {
		t.Fatalf("the first frame with spheres and planes uploaded %d buffers, want 9", h.backend.bakes)
	}
	h.backend.bakes = 0
	h.frame()
	if h.backend.bakes != 5 {
		t.Fatalf("a steady frame uploaded %d buffers, want the five arenas", h.backend.bakes)
	}

	pass := h.passes()[0]
	meshes := map[uint32]int{}
	for _, batch := range pass.Batches {
		meshes[batch.MeshID]++
	}
	if len(meshes) != 3 {
		t.Fatalf("the frame drew %d distinct meshes, want the box, the sphere and the plane", len(meshes))
	}
}

// A line is the unit box stretched along its local X from start to end and to
// the thickness across it, and the packed instance carries SCENE_NONUNIFORM
// for it, which is the shader's inverse-transpose path.
func TestALineIsAStretchedBoxFromStartToEnd(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end m.Vec3
	}{
		{name: "along x", start: m.Vec3{X: -1}, end: m.Vec3{X: 3}},
		{name: "straight up", start: m.Vec3{Y: 1}, end: m.Vec3{Y: 4}},
		{name: "straight down", start: m.Vec3{}, end: m.Vec3{Y: -2}},
		{name: "diagonal", start: m.Vec3{X: 1, Y: 2, Z: 3}, end: m.Vec3{X: -2, Y: 0.5, Z: 7}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var q scene.OpQueue
			q.Line3D(0, test.start, test.end, 0.2, testLineColor)
			if len(types.OpQueueRecordedDraws(&q)) != 1 {
				t.Fatalf("recorded %d draws, want 1", len(types.OpQueueRecordedDraws(&q)))
			}
			record := types.OpQueueRecordedDraws(&q)[0]
			if record.Matrix != nil {
				t.Fatal("the line went through a whole-matrix override; scene builds its stretch from the transform")
			}
			world := record.World()
			if got := world.TransformPoint(m.Vec3{X: -0.5}); !nearVec3(got, test.start) {
				t.Fatalf("the box's -X face centre lands at %v, want start %v", got, test.start)
			}
			if got := world.TransformPoint(m.Vec3{X: 0.5}); !nearVec3(got, test.end) {
				t.Fatalf("the box's +X face centre lands at %v, want end %v", got, test.end)
			}
			axis := test.end.Sub(test.start).Normalize()
			for _, across := range []m.Vec3{{Y: 0.5}, {Z: 0.5}} {
				offset := world.TransformDirection(across)
				if !near(offset.Dot(axis), 0) || !near(offset.Length(), 0.1) {
					t.Fatalf("the box's half-extent %v maps to %v, want 0.1 across the line", across, offset)
				}
			}
			if model.PackInstance(world, model.InstanceAnim{Offset: model.SceneNoAnim}, 0).Flags&model.SceneNonUniform == 0 {
				t.Fatal("a line's instance lacks SCENE_NONUNIFORM")
			}
		})
	}
}

// Box, Sphere and Plane are lit paint; Line3D and WireBox are black paint that
// glows the given colour, so they show in a frame with no lights.
func TestLinesAreSelfLitAndSolidsAreLit(t *testing.T) {
	var q scene.OpQueue
	q.Box(0, m.At(0, 0, 0), testBoxColor)
	q.Sphere(0, m.Vec3{}, 1, testBoxColor)
	q.Plane(0, m.Vec3{}, m.Vec2{X: 1, Y: 1}, testBoxColor)
	q.Line3D(0, m.Vec3{}, m.Vec3{X: 1}, 0.1, testLineColor)
	q.WireBox(0, m.Vec3{}, m.Vec3{X: 1, Y: 1, Z: 1}, 0.1, testLineColor)

	paint := m.Vec4{X: testBoxColor.R, Y: testBoxColor.G, Z: testBoxColor.B, W: testBoxColor.A}
	glow := m.Vec4{X: testLineColor.R, Y: testLineColor.G, Z: testLineColor.B}
	for i, record := range types.OpQueueRecordedDraws(&q) {
		pbr := record.PbrRecord()
		if pbr.MetallicFactor != 0 {
			t.Fatalf("draw %d is metallic %v, want paint", i, pbr.MetallicFactor)
		}
		if i < 3 {
			if pbr.BaseColorFactor != paint || pbr.EmissiveFactor != (m.Vec4{}) {
				t.Fatalf("lit draw %d has base %v and emissive %v, want the colour and none", i, pbr.BaseColorFactor, pbr.EmissiveFactor)
			}
			continue
		}
		if pbr.BaseColorFactor != (m.Vec4{W: 1}) || pbr.EmissiveFactor != glow {
			t.Fatalf("self-lit draw %d has base %v and emissive %v, want black and the colour", i, pbr.BaseColorFactor, pbr.EmissiveFactor)
		}
	}
}

// A sphere's world sphere is its radius exactly, not the circumsphere of its
// box, and a plane's is the circumsphere of its rectangle.
func TestSpheresAndPlanesCullByTheirOwnBounds(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		// Behind the camera by 5; a sphere of radius 4.9 stays behind it and a
		// sphere of radius 5.1 reaches it. The unit box's circumsphere would
		// have let a radius of 2.9 through.
		q.Sphere(0, m.Vec3{Z: 5}, 4.9, testBoxColor)
		q.Sphere(0, m.Vec3{Z: 5}, 5.1, testBoxColor)
		// A 12 x 12 plane 5 behind the camera reaches it along its diagonal.
		q.Plane(0, m.Vec3{Z: 5}, m.Vec2{X: 12, Y: 12}, testBoxColor)
		q.Plane(0, m.Vec3{Z: 5}, m.Vec2{X: 6, Y: 6}, testBoxColor)
	})
	h.frame()

	if pass := h.passes()[0]; pass.Recorded != 4 || pass.Culled != 2 || pass.Instances != 2 {
		t.Fatalf("recorded %d, culled %d, packed %d; want 4, 2, 2", pass.Recorded, pass.Culled, pass.Instances)
	}
}

// A sphere scales uniformly, so it rides the plain normal path; a plane is
// stretched and takes the inverse-transpose path like a line.
func TestOnlyStretchedShapesFlagNonUniform(t *testing.T) {
	var q scene.OpQueue
	q.Sphere(0, m.Vec3{X: 1}, 3, testBoxColor)
	q.Plane(0, m.Vec3{}, m.Vec2{X: 4, Y: 2}, testBoxColor)
	sphere, plane := types.OpQueueRecordedDraws(&q)[0], types.OpQueueRecordedDraws(&q)[1]

	if flags := model.PackInstance(sphere.World(), model.InstanceAnim{Offset: model.SceneNoAnim}, 0).Flags; flags&model.SceneNonUniform != 0 {
		t.Fatalf("a sphere's instance carries SCENE_NONUNIFORM (%#b)", flags)
	}
	if got := sphere.World().TransformPoint(m.Vec3{Y: 1}); !nearVec3(got, m.Vec3{X: 1, Y: 3}) {
		t.Fatalf("the unit sphere's pole lands at %v, want (1,3,0) for radius 3 at x = 1", got)
	}
	if flags := model.PackInstance(plane.World(), model.InstanceAnim{Offset: model.SceneNoAnim}, 0).Flags; flags&model.SceneNonUniform == 0 {
		t.Fatalf("a 4 x 2 plane's instance lacks SCENE_NONUNIFORM (%#b)", flags)
	}
	if got := plane.World().TransformPoint(m.Vec3{X: 0.5, Z: 0.5}); !nearVec3(got, m.Vec3{X: 2, Z: 1}) {
		t.Fatalf("the plane's corner lands at %v, want (2,0,1)", got)
	}
}

// The twelve edges together span the box grown by half a thickness on every
// side, which is what closes the corners.
func TestWireBoxEdgesCloseAtTheCorners(t *testing.T) {
	var q scene.OpQueue
	center, size, thickness := m.Vec3{X: 1, Y: 2, Z: 3}, m.Vec3{X: 2, Y: 4, Z: 6}, float32(0.2)
	q.WireBox(0, center, size, thickness, testLineColor)
	if len(types.OpQueueRecordedDraws(&q)) != 12 {
		t.Fatalf("recorded %d draws, want 12", len(types.OpQueueRecordedDraws(&q)))
	}
	extent := m.Box3{Min: center, Max: center}
	for _, record := range types.OpQueueRecordedDraws(&q) {
		world := record.World()
		for _, corner := range []m.Vec3{{X: -0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: 0.5, Z: 0.5}} {
			point := world.TransformPoint(corner)
			extent.Min, extent.Max = extent.Min.Min(point), extent.Max.Max(point)
		}
		if model.PackInstance(world, model.InstanceAnim{Offset: model.SceneNoAnim}, 0).Flags&model.SceneNonUniform == 0 {
			t.Fatal("an edge's instance lacks SCENE_NONUNIFORM")
		}
	}
	grown := size.MulS(0.5).AddS(thickness / 2)
	if !nearVec3(extent.Min, center.Sub(grown)) || !nearVec3(extent.Max, center.Add(grown)) {
		t.Fatalf("the edges span %v..%v, want %v..%v", extent.Min, extent.Max, center.Sub(grown), center.Add(grown))
	}
}

// Every debug shape binds the bundled material with the five PBR slots, the
// same as a box: one shader, one topology, everything batching together.
func TestEveryDebugShapeBindsTheBundledMaterial(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Sphere(0, m.Vec3{}, 1, testBoxColor)
		q.Plane(0, m.Vec3{}, m.Vec2{X: 1, Y: 1}, testBoxColor)
		q.Line3D(0, m.Vec3{}, m.Vec3{X: 1}, 0.1, testLineColor)
	})
	h.frame()

	pass := h.passes()[0]
	if pass.Instances != 3 {
		t.Fatalf("packed %d instances, want 3", pass.Instances)
	}
	for _, batch := range pass.Batches {
		if batch.MaterialID != pass.Batches[0].MaterialID {
			t.Fatalf("the shapes bound different materials: %+v", pass.Batches)
		}
	}
	if textures := h.backend.texturesBoundTo("baseColorTexture"); len(textures) != 3 {
		t.Fatalf("baseColorTexture was bound %d times, want once per shape", len(textures))
	}
	if materials := h.backend.buffersBoundTo("scenePbrMaterial"); len(materials) != 3 {
		t.Fatalf("scenePbrMaterial was bound %d times, want once per shape", len(materials))
	}
}

func nearVec3(a, b m.Vec3) bool {
	return math.Abs(float64(a.X-b.X)) < 1e-4 && math.Abs(float64(a.Y-b.Y)) < 1e-4 && math.Abs(float64(a.Z-b.Z)) < 1e-4
}

// A wire box's twelve edges share one mesh and one material, and stay twelve
// single-instance batches. The batching the flush does is per instanced call;
// collapsing consecutive equal draws recorded one at a time is deferred, and a
// wire box is twelve separate draws by design.
func TestAWireBoxStaysTwelveSingleInstanceBatches(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.WireBox(0, m.Vec3{Z: -5}, m.Vec3{X: 1, Y: 1, Z: 1}, 0.05, testLineColor)
	})
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 12 {
		t.Fatalf("published %d batches, want the twelve edges: %+v", len(batches), batches)
	}
	for i, batch := range batches {
		if batch.InstanceCount != 1 {
			t.Fatalf("edge %d is a batch of %d instances, want 1", i, batch.InstanceCount)
		}
	}
}
