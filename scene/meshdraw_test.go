package scene

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// customVertex is a caller's own layout: position only, at location 0. It is
// deliberately not a prefix of scene.Vertex in size, so a mesh built from it
// cannot be mistaken for a standard one by byte count either.
type customVertex struct{ Position m.Vec3 }

func (customVertex) VertexLayout() []gfx.VertexAttr {
	return []gfx.VertexAttr{gfx.Attr(0, gfx.Float32x3)}
}

// triangle is the smallest valid standard-layout mesh: one counter-clockwise
// face large enough that a camera at the default distance keeps it.
func triangle() []Vertex {
	return []Vertex{
		{Position: m.Vec3{X: -1, Y: -1}, Normal: m.Vec3{Z: 1}, Color: m.White},
		{Position: m.Vec3{X: 1, Y: -1}, Normal: m.Vec3{Z: 1}, Color: m.White},
		{Position: m.Vec3{Y: 1}, Normal: m.Vec3{Z: 1}, Color: m.White},
	}
}

// bake runs one BakeMesh through a scoped LookupAccess, the way a real caller
// would from a handler holding the Lookup write lock.
func (h *harness) bake(vertices []Vertex, indices []uint32, topology gfx.PrimitiveTopology) MeshRef {
	var ref MeshRef
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{run: func(la LookupAccess) {
		ref = la.BakeMesh(vertices, indices, topology)
	}})
	return ref
}

func (h *harness) lookup(run func(LookupAccess)) {
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{run: run})
}

// reportedAs finds the first reported error of a given type.
func reportedAs[E error](reported []error) (E, bool) {
	var want E
	for _, err := range reported {
		if errors.As(err, &want) {
			return want, true
		}
	}
	return want, false
}

// The floor of the API: build vertices in Go, hand them to scene, draw them.
// The ref is minted and returned immediately, and the mesh baked in the same
// handler still uploads and draws in that frame.
func TestABakedMeshDrawsInTheFrameItWasBakedIn(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, MeshDraw{})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	if ref.ID() == 0 {
		t.Fatal("BakeMesh returned no ref")
	}
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("packed %d instances, want the baked triangle", pass.Instances)
	}
	if len(h.backend.draws) != 1 {
		t.Fatalf("the backend received %d draws, want 1", len(h.backend.draws))
	}
	if draw := h.backend.draws[0]; !draw.indexed || draw.count != 3 {
		t.Fatalf("drew %d indexed %v, want 3 indices", draw.count, draw.indexed)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// A temporary mesh needs no Lookup at all: it is minted into the recording and
// gone at the end of the frame.
func TestATemporaryMeshDrawsWithoutBaking(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		if ref.ID()&temporaryMeshID == 0 {
			t.Errorf("a temporary ref's id %d does not carry the temporary bit", ref.ID())
		}
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
	})
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("packed %d instances, want the temporary triangle", pass.Instances)
	}
	if draw := h.backend.draws[0]; draw.indexed || draw.count != 3 {
		t.Fatalf("drew %d indexed %v, want 3 non-indexed vertices", draw.count, draw.indexed)
	}
}

// The two sources allocate independent dense ranges, and the sort key is one
// uint32 of meshID; without a bit to tell them apart the first durable and the
// first temporary mesh would batch as though they were one geometry.
func TestDurableAndTemporaryIdsDoNotCollide(t *testing.T) {
	var durable MeshRef
	var temporary MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		temporary = q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		q.Mesh(0, durable, MeshDraw{NeverCull: true})
		q.Mesh(0, temporary, MeshDraw{NeverCull: true})
	})
	durable = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	if durable.ID() == temporary.ID() {
		t.Fatalf("both meshes report id %d", durable.ID())
	}
	batches := h.passes()[0].Batches
	if len(batches) != 2 || batches[0].MeshID == batches[1].MeshID {
		t.Fatalf("published %d batches with mesh ids %v", len(batches), batches)
	}
}

// The zero value of MeshDraw draws at the origin with the bundled PBR and culls
// by the mesh's own baked sphere. That sphere is computed once, at bake time,
// from the standard layout's positions.
func TestABakedStandardMeshCullsByItsOwnSphere(t *testing.T) {
	var near, far MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, near, MeshDraw{Transform: At(0, 0, -5)})
		q.Mesh(0, far, MeshDraw{Transform: At(0, 0, 5)})
	})
	near = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	far = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	if pass := h.passes()[0]; pass.Recorded != 2 || pass.Culled != 1 || pass.Instances != 1 {
		t.Fatalf("recorded %d, culled %d, packed %d; want 2, 1, 1",
			pass.Recorded, pass.Culled, pass.Instances)
	}
}

// A temporary mesh never gets a sphere of its own: the pass is O(n) over
// geometry that is thrown away at the end of the frame. It falls through to
// never-cull, so a draw that should cull says so with explicit Bounds.
func TestATemporaryMeshIsNeverCulledUnlessItSaysSo(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		behind := q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		q.Mesh(0, behind, MeshDraw{Transform: At(0, 0, 5)})
	})
	h.frame()
	if pass := h.passes()[0]; pass.Culled != 0 || pass.Instances != 1 {
		t.Fatalf("without bounds: culled %d, packed %d; want 0, 1", pass.Culled, pass.Instances)
	}

	bounded := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		behind := q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		q.Mesh(0, behind, MeshDraw{Transform: At(0, 0, 5), Bounds: m.Vec4{W: 1}})
	})
	bounded.frame()
	if pass := bounded.passes()[0]; pass.Culled != 1 || pass.Instances != 0 {
		t.Fatalf("with bounds: culled %d, packed %d; want 1, 0", pass.Culled, pass.Instances)
	}
}

// Invalid geometry is reported once and yields a zero MeshRef, which then draws
// nothing. It departs from canvas.DrawTriangles, which silently returns,
// because a bake happens once where recording happens every frame.
func TestInvalidGeometryIsReportedAndYieldsNoRef(t *testing.T) {
	cases := []struct {
		name     string
		vertices []Vertex
		indices  []uint32
		topology gfx.PrimitiveTopology
	}{
		{name: "no vertices", topology: gfx.TopologyTriangleList},
		{name: "index out of range", vertices: triangle(), indices: []uint32{0, 1, 3}},
		{name: "indices not a multiple of three", vertices: triangle(), indices: []uint32{0, 1}},
		{name: "vertices not a multiple of three", vertices: triangle()[:2]},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newHarness(t, func(*OpQueue) {})
			ref := h.bake(testCase.vertices, testCase.indices, testCase.topology)
			if ref.ID() != 0 {
				t.Fatalf("minted ref %d for invalid geometry", ref.ID())
			}
			if _, ok := reportedAs[ErrMeshGeometryInvalid](*h.reported); !ok {
				t.Fatalf("reported %v, want an ErrMeshGeometryInvalid", *h.reported)
			}
		})
	}
}

// A rejected mint yields a zero ref, and drawing one is silent: the mint that
// produced it already reported, so reporting again would be one report per
// frame for a mistake made once.
func TestDrawingARejectedMintIsSilent(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, MeshDraw{})
	})
	ref = h.bake(nil, nil, gfx.TopologyTriangleList)
	*h.reported = (*h.reported)[:0]
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances for a zero ref", pass.Instances)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v for a ref whose mint already reported", *h.reported)
	}
}

// A custom vertex layout requires a custom material: the bundled PBR is one
// shader module with one vertex stage and no entry-point selection, so its
// inputs are scene.Vertex's eight attributes and nothing else.
func TestACustomLayoutWithTheBundledMaterialIsReportedAndSkipped(t *testing.T) {
	custom := []customVertex{{Position: m.Vec3{X: -1, Y: -1}}, {Position: m.Vec3{X: 1, Y: -1}}, {}}
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(custom, nil, gfx.TopologyTriangleList)
		// Three draws of the one ref, because the report is keyed by ref.
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
	})
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances, want the custom-layout draws skipped", pass.Instances)
	}
	if len(*h.reported) != 1 {
		t.Fatalf("reported %v, want exactly one report for the one ref", *h.reported)
	}
	if _, ok := reportedAs[ErrMeshCustomLayoutNeedsMaterial](*h.reported); !ok {
		t.Fatalf("reported %v, want an ErrMeshCustomLayoutNeedsMaterial", *h.reported)
	}
}

// The reverse is fine: the standard layout with a custom material draws.
func TestACustomLayoutDrawsWithACustomMaterial(t *testing.T) {
	custom := []customVertex{{Position: m.Vec3{X: -1, Y: -1}}, {Position: m.Vec3{X: 1, Y: -1}}, {}}
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(custom, nil, gfx.TopologyTriangleList)
		q.Mesh(0, ref, MeshDraw{Material: opaqueMaterial(7), NeverCull: true})
	})
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("packed %d instances, want the custom mesh drawn", pass.Instances)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// UpdateMesh is a wholesale re-bake at any size, deferred the same way, and it
// recomputes the bounding sphere - so a mesh that grows past its old bounds
// still culls correctly.
func TestUpdateMeshRebakesAtAnySizeAndRecomputesTheSphere(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, MeshDraw{Transform: At(0, 0, -110)})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()
	if pass := h.passes()[0]; pass.Culled != 1 {
		t.Fatalf("a small mesh past Far: culled %d, want 1", pass.Culled)
	}

	// Six vertices reaching far enough that the sphere now crosses Far.
	huge := append(triangle(), Vertex{Position: m.Vec3{X: 30}}, Vertex{Position: m.Vec3{Y: 30}}, Vertex{Position: m.Vec3{Z: 30}})
	h.lookup(func(la LookupAccess) {
		if !la.UpdateMesh(ref, huge, nil) {
			t.Error("UpdateMesh refused a same-layout, same-topology update")
		}
	})
	h.frame()

	if pass := h.passes()[0]; pass.Culled != 0 || pass.Instances != 1 {
		t.Fatalf("after the update: culled %d, packed %d; want 0, 1", pass.Culled, pass.Instances)
	}
	if draw := h.backend.draws[len(h.backend.draws)-1]; draw.count != 6 {
		t.Fatalf("drew %d vertices after the update, want 6", draw.count)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// The pipeline key and the meshID both assume the layout is fixed for a ref's
// life, so an update that changes it is refused rather than applied.
func TestUpdateMeshRefusesALayoutChangeAndATemporaryRef(t *testing.T) {
	var temporary MeshRef
	h := newHarness(t, func(q *OpQueue) {
		temporary = q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
	})
	h.frame()
	ref := h.bake(triangle(), nil, gfx.TopologyTriangleList)

	h.lookup(func(la LookupAccess) {
		custom := []customVertex{{Position: m.Vec3{X: -1}}, {Position: m.Vec3{X: 1}}, {}}
		if la.UpdateMesh(ref, custom, nil) {
			t.Error("UpdateMesh accepted a layout change")
		}
		if la.UpdateMesh(temporary, triangle(), nil) {
			t.Error("UpdateMesh accepted a temporary ref")
		}
	})

	rejections := 0
	for _, err := range *h.reported {
		var rejected ErrMeshUpdateRejected
		if errors.As(err, &rejected) {
			rejections++
		}
	}
	if rejections != 2 {
		t.Fatalf("reported %v, want two ErrMeshUpdateRejected", *h.reported)
	}
}

// ReleaseMesh is explicit and generation-counted. The ref goes stale at once,
// and drawing it is reported once per ref rather than once per draw.
func TestDrawingAReleasedMeshIsReportedOnceAndSkipped(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()
	h.lookup(func(la LookupAccess) { la.ReleaseMesh(ref) })
	*h.reported = (*h.reported)[:0]
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances of a released mesh", pass.Instances)
	}
	if len(*h.reported) != 1 {
		t.Fatalf("reported %v, want one report for the two draws of one ref", *h.reported)
	}
	if _, ok := reportedAs[ErrMeshUnavailable](*h.reported); !ok {
		t.Fatalf("reported %v, want an ErrMeshUnavailable", *h.reported)
	}
}

// The generation counter is what makes a recycled id detectable rather than a
// draw of whatever now occupies that slot.
func TestAStaleRefDoesNotDrawTheMeshThatReusedItsSlot(t *testing.T) {
	var stale, reused MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, stale, MeshDraw{NeverCull: true})
	})
	stale = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()
	h.lookup(func(la LookupAccess) { la.ReleaseMesh(stale) })
	reused = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	if reused.id != stale.id {
		t.Fatalf("the released slot %d was not reissued, got %d", stale.id, reused.id)
	}
	*h.reported = (*h.reported)[:0]
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances, want the stale ref skipped", pass.Instances)
	}
	if _, ok := reportedAs[ErrMeshUnavailable](*h.reported); !ok {
		t.Fatalf("reported %v, want an ErrMeshUnavailable", *h.reported)
	}
}

// source is what makes a temporary ref used in a later frame detectable rather
// than silently wrong: id and slot alike survive, only the frame stamp does not.
func TestATemporaryRefFromAnEarlierFrameIsReportedAndSkipped(t *testing.T) {
	var kept MeshRef
	frames := 0
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		frames++
		// Every frame mints a temporary, so the kept ref's slot is occupied in
		// the later frame too: only the frame stamp can tell them apart.
		fresh := q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		if frames == 1 {
			kept = fresh
		}
		q.Mesh(0, kept, MeshDraw{NeverCull: true})
	})
	h.frame()
	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("the frame that minted it packed %d instances, want 1", pass.Instances)
	}
	*h.reported = (*h.reported)[:0]
	h.frame()

	if kept.id != 1 {
		t.Fatalf("the kept ref has id %d, so the later frame's mesh did not reuse its slot", kept.id)
	}
	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("a later frame packed %d instances of a temporary ref", pass.Instances)
	}
	if _, ok := reportedAs[ErrMeshUnavailable](*h.reported); !ok {
		t.Fatalf("reported %v, want an ErrMeshUnavailable", *h.reported)
	}
}

// Every gfx topology passes straight through, and the zero value is a triangle
// list. A line list is not checked for a multiple of three.
func TestTopologyPassesThroughAndOnlyTriangleListsDivideByThree(t *testing.T) {
	h := newHarness(t, func(*OpQueue) {})
	ref := h.bake(triangle()[:2], nil, gfx.TopologyLineList)
	if ref.ID() == 0 {
		t.Fatalf("a two-vertex line list was rejected: %v", *h.reported)
	}
	h.lookup(func(la LookupAccess) {
		var record meshRecord
		record = la.lookup.meshes[ref.id-1]
		if record.topology != gfx.TopologyLineList {
			t.Errorf("recorded topology %v, want the line list", record.topology)
		}
	})
}

// Transforms places one instance per entry, overriding the single Transform.
// Culling is per instance, and Ops still reports the one call the recorder made.
func TestTransformsPlaceOneInstanceEachAndCullIndependently(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, MeshDraw{
			Transform:  At(0, 0, 500),
			Transforms: []Transform{At(0, 0, -5), At(2, 0, -5), At(0, 0, 5)},
		})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	if pass := h.passes()[0]; pass.Recorded != 3 || pass.Culled != 1 || pass.Instances != 2 {
		t.Fatalf("recorded %d, culled %d, packed %d; want 3, 1, 2",
			pass.Recorded, pass.Culled, pass.Instances)
	}
	ops := h.ops()
	if len(ops) != 2 || ops[1].Kind != OpMesh {
		t.Fatalf("reported %d ops, want the camera and one OpMesh", len(ops))
	}
	if len(ops[1].Draw.Transforms) != 3 || ops[1].Mesh.ID() != ref.ID() {
		t.Fatalf("the op reports mesh %d and %d transforms", ops[1].Mesh.ID(), len(ops[1].Draw.Transforms))
	}
}

// Params are bound on top of the three ranges scene binds itself, which is what
// lets a custom material declare bindings scene knows nothing about.
func TestMeshDrawParamsReachTheBackend(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(triangle(), nil, gfx.TopologyTriangleList)
		q.Mesh(0, ref, MeshDraw{
			Material:  opaqueMaterial(3),
			Params:    []gfx.ParameterDescr{gfx.FloatParam("key", 9)},
			NeverCull: true,
		})
	})
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("packed %d instances", pass.Instances)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// A mesh draw carries no colour of its own, so what it takes from the bundled
// PBR is glTF's white base colour - and metallic zero, because glTF's fully
// metallic default has no diffuse at all and scene has no environment to
// reflect, so a mesh with nothing said about it would render black.
func TestAMeshDrawTakesWhiteNonMetallicPaintFromTheBundledPbr(t *testing.T) {
	record := drawRecord{mesh: MeshRef{source: meshDurable, id: 1, generation: 1}}.pbrRecord()
	if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor is %v, want white", record.BaseColorFactor)
	}
	if record.MetallicFactor != 0 {
		t.Errorf("metallicFactor is %v, want zero", record.MetallicFactor)
	}
	if record.EmissiveFactor != (m.Vec4{}) {
		t.Errorf("emissiveFactor is %v, want nothing: a mesh draw is not self-lit", record.EmissiveFactor)
	}
	// A debug shape still paints with its own colour through the same record.
	shape := drawRecord{shape: shapeBox, color: m.NewColorSrgb(1, 0, 0, 1)}.pbrRecord()
	if shape.BaseColorFactor.X == 1 && shape.BaseColorFactor.Y == 1 {
		t.Errorf("a debug box painted %v, want its own colour", shape.BaseColorFactor)
	}
}

// An instanced opaque draw is one batch of its surviving instances: one gfx
// draw call, one material record, and the survivors packed contiguously from
// the batch's own firstInstance.
func TestAnInstancedOpaqueDrawIsOneBatchOfItsSurvivors(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, MeshDraw{Transforms: []Transform{
			At(0, 0, -5), At(0, 0, 5), At(2, 0, -5), At(-2, 0, -5),
		}})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	pass := h.passes()[0]
	if pass.Recorded != 4 || pass.Culled != 1 || pass.Instances != 3 {
		t.Fatalf("recorded %d, culled %d, packed %d; want 4, 1, 3",
			pass.Recorded, pass.Culled, pass.Instances)
	}
	if len(pass.Batches) != 1 {
		t.Fatalf("published %d batches, want the instanced call as one: %+v",
			len(pass.Batches), pass.Batches)
	}
	if batch := pass.Batches[0]; batch.FirstInstance != 0 || batch.InstanceCount != 3 {
		t.Fatalf("the batch spans instances [%d, %d), want [0, 3)",
			batch.FirstInstance, batch.FirstInstance+batch.InstanceCount)
	}
	if len(h.backend.draws) != 1 {
		t.Fatalf("the backend saw %d draw calls, want the one batch", len(h.backend.draws))
	}
	if draw := h.backend.draws[0]; draw.firstInstance != 0 || draw.instances != 3 {
		t.Fatalf("drew %d instances from %d, want 3 from 0", draw.instances, draw.firstInstance)
	}
	if materials := h.backend.buffersBoundTo("scenePbrMaterial"); len(materials) != 1 {
		t.Fatalf("scenePbrMaterial was bound %d times, want once per batch", len(materials))
	}
}

// A blend-class instanced draw stays one sort entry, and so one batch, per
// instance: sorting the set by its nearest instance would composite visibly
// wrong, so the split survives the collapse the opaque class gets.
func TestAnInstancedBlendDrawStaysOneBatchPerInstance(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, MeshDraw{
			Material:   blendMaterial(1),
			Transforms: []Transform{At(0, 0, -2), At(0, 0, -20), At(0, 0, -8)},
		})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	pass := h.passes()[0]
	if pass.Instances != 3 {
		t.Fatalf("packed %d instances, want 3", pass.Instances)
	}
	if len(pass.Batches) != 3 {
		t.Fatalf("published %d batches, want one per instance: %+v", len(pass.Batches), pass.Batches)
	}
	for i, batch := range pass.Batches {
		if batch.FirstInstance != i || batch.InstanceCount != 1 {
			t.Fatalf("batch %d spans instances [%d, %d), want the single instance %d",
				i, batch.FirstInstance, batch.FirstInstance+batch.InstanceCount, i)
		}
	}
}

// Two instanced calls of one mesh with one material stay two batches. What the
// flush collapses is the instances of one call; collapsing consecutive equal
// draws that were recorded separately is deferred.
func TestTwoInstancedCallsStayTwoBatches(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, MeshDraw{Transforms: []Transform{At(0, 0, -5), At(1, 0, -5)}})
		q.Mesh(0, ref, MeshDraw{Transforms: []Transform{At(2, 0, -5), At(3, 0, -5)}})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want one per call: %+v", len(batches), batches)
	}
	if batches[0].FirstInstance != 0 || batches[0].InstanceCount != 2 ||
		batches[1].FirstInstance != 2 || batches[1].InstanceCount != 2 {
		t.Fatalf("the batches span %+v, want [0, 2) and [2, 4)", batches)
	}
}

// Sorting is per pass, so an instanced draw two cameras both see is packed once
// per pass - two batches of N rather than one shared batch. That is the cost of
// the sort, and it is not worked around.
func TestAnInstancedDrawIsPackedOncePerPass(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Camera(testCamera+1, forwardCamera())
		q.Mesh(0, ref, MeshDraw{Transforms: []Transform{At(0, 0, -5), At(1, 0, -5)}})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	passes := h.passes()
	if len(passes) != 2 {
		t.Fatalf("published %d passes, want one per camera", len(passes))
	}
	for i, pass := range passes {
		if len(pass.Batches) != 1 {
			t.Fatalf("pass %d published %+v, want one batch", i, pass.Batches)
		}
		if batch := pass.Batches[0]; batch.FirstInstance != 0 || batch.InstanceCount != 2 {
			t.Fatalf("pass %d packed instances [%d, %d), want [0, 2) of its own slice",
				i, batch.FirstInstance, batch.FirstInstance+batch.InstanceCount)
		}
	}
}
