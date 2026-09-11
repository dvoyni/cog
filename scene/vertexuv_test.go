package scene

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// decodeUV is the transcription of sceneDecodeUV in builtin/scene/vertexdecode.wgsl,
// with the fetch unit's divide in front of it. Every assertion below reads a
// stored UV through this, so what is measured is the coordinate a fragment
// stage samples with rather than the code the packer wrote.
func decodeUV(code [2]uint16, scale, bias m.Vec2) m.Vec2 {
	return m.Vec2{
		X: float32(code[0])/uvCodeMax*scale.X + bias.X,
		Y: float32(code[1])/uvCodeMax*scale.Y + bias.Y,
	}
}

// roundTripUV encodes one coordinate against a range and reads it back.
func roundTripUV(uv m.Vec2, scale, bias m.Vec2) m.Vec2 {
	return decodeUV([2]uint16{
		quantizeUV(uv.X, scale.X, bias.X),
		quantizeUV(uv.Y, scale.Y, bias.Y),
	}, scale, bias)
}

// A mesh with no UVs at all - a debug shape, a procedural hull, scene's own
// unit meshes on their second set - derives the empty record, which is what
// points it at slot 0. Slot 0 is the identity, and a code of zero through it is
// zero, so the two answers coincide with nothing special-cased at draw time.
func TestAMeshWithNoUVsDerivesTheEmptyRecordAndDecodesAsANoOp(t *testing.T) {
	var uv0, uv1 uvRange
	for range 4 {
		uv0.add(m.Vec2{})
		uv1.add(m.Vec2{})
	}
	record := meshRecordFor(uv0, uv1)
	if record != (sceneMesh{}) {
		t.Fatalf("a UV-less mesh derived %+v, want the empty record that names slot 0", record)
	}
	if got := roundTripUV(m.Vec2{}, identityMesh.UV0Scale, identityMesh.UV0Bias); got != (m.Vec2{}) {
		t.Fatalf("slot 0 decoded a zero UV to %v, want the origin", got)
	}
	// A mesh scene never packed - a custom layout - has the same empty record,
	// because nothing ever wrote one.
	if (meshRecord{}).uv != (sceneMesh{}) {
		t.Fatal("a custom-layout mesh carries a record of its own, want the empty one")
	}
}

// A UV set collapsed to a single point stores scale 0 and bias equal to that
// point, and the ordinary decode returns it exactly. CesiumMilkTruck has two
// such primitives, so this is authoring rather than a hypothetical.
func TestAZeroWidthRangeDecodesToTheExactConstant(t *testing.T) {
	var uv0 uvRange
	for range 8 {
		uv0.add(m.Vec2{X: 0, Y: 1})
	}
	scale, bias := uv0.scaleBias()
	if scale != (m.Vec2{}) || bias != (m.Vec2{X: 0, Y: 1}) {
		t.Fatalf("a collapsed set derived scale %v bias %v, want zero and the point", scale, bias)
	}
	if got := roundTripUV(m.Vec2{X: 0, Y: 1}, scale, bias); got != (m.Vec2{X: 0, Y: 1}) {
		t.Fatalf("the collapsed set decoded to %v, want the constant (0, 1) exactly", got)
	}
}

// The two endpoints of a range are the two values that have to survive exactly,
// because a UV island reaching the atlas edge is where a half float loses a
// whole binade.
func TestTheEndsOfAUVRangeRoundTripExactly(t *testing.T) {
	var uv uvRange
	for _, point := range []m.Vec2{{X: -0.5, Y: 0.25}, {X: 18.52, Y: -13.49}, {X: 3, Y: 0}} {
		uv.add(point)
	}
	scale, bias := uv.scaleBias()
	for _, end := range []m.Vec2{{X: -0.5, Y: -13.49}, {X: 18.52, Y: 0.25}} {
		if got := roundTripUV(end, scale, bias); got != end {
			t.Errorf("the range end %v decoded to %v, want it back exactly", end, got)
		}
	}
}

// The whole reason the range is carried rather than a half float taking the
// same four bytes: at a 4096-texel texture the tiled outlier the corpus
// actually ships lands inside one texel where a half float is 64 out.
//
// The outlier is EmissiveStrengthTest 1.0, whose TEXCOORD_0 reaches
// (18.52, -13.49); the binade case is an island touching exactly 1.0.
func TestAQuantisedUVBeatsAHalfFloatOnTheTiledOutlier(t *testing.T) {
	const texels = 4096
	for _, c := range []struct {
		what      string
		corners   [2]m.Vec2
		halfFloat float32
		want      float32
	}{
		{"the worst tiled outlier", [2]m.Vec2{{X: -13.49, Y: -13.49}, {X: 18.52, Y: 18.52}}, 64, 1.1},
		{"an island touching exactly 1.0", [2]m.Vec2{{}, {X: 1, Y: 1}}, 4, 0.07},
	} {
		var uv uvRange
		uv.add(c.corners[0])
		uv.add(c.corners[1])
		scale, bias := uv.scaleBias()
		worst := float32(0)
		// Sample the range densely rather than at its ends, which are exact by
		// construction: the error to measure is the one in the middle.
		const samples = 4096
		for i := range samples + 1 {
			at := c.corners[0].X + (c.corners[1].X-c.corners[0].X)*float32(i)/samples
			back := roundTripUV(m.Vec2{X: at, Y: at}, scale, bias)
			worst = max(worst, abs32(back.X-at)*texels)
		}
		if worst > c.want {
			t.Errorf("%s: worst error %.3f texels at %d, want at most %.3f (a half float is %.0f)",
				c.what, worst, texels, c.want, c.halfFloat)
		}
		if halfError := halfFloatError(c.corners[1].X) * texels; halfError < worst {
			t.Errorf("%s: a half float is %.3f texels and the range is %.3f - the range bought nothing",
				c.what, halfError, worst)
		}
	}
}

// halfFloatError is one ULP of an IEEE binary16 at the given magnitude, which
// is the quantisation step a Float16x2 UV would have carried at that value.
func halfFloatError(value float32) float32 {
	magnitude := math.Abs(float64(value))
	if magnitude == 0 {
		return 0
	}
	// A binary16 has 10 explicit mantissa bits, so its step across a binade is
	// 2^(exponent-10).
	exponent := math.Floor(math.Log2(magnitude))
	return float32(math.Ldexp(1, int(exponent)-10))
}

// The record is derived from the vertices the mesh actually carries, on the
// path an app authors through, and it is re-derived when those vertices are
// replaced: precision follows the spread of the bake rather than a number
// anybody supplied.
func TestPackingTheVerticesDerivesTheirUVRange(t *testing.T) {
	vertices := []Vertex{
		{UV0: m.Vec2{X: 0.25, Y: 2}, UV1: m.Vec2{X: -1, Y: -1}},
		{UV0: m.Vec2{X: 4, Y: -0.5}, UV1: m.Vec2{X: -1, Y: -1}},
	}
	var arena []byte
	at, _, record := packVertices(&arena, vertices)
	if want := (m.Vec2{X: 3.75, Y: 2.5}); record.UV0Scale != want {
		t.Fatalf("uv0 scale %v, want the spread %v", record.UV0Scale, want)
	}
	if want := (m.Vec2{X: 0.25, Y: -0.5}); record.UV0Bias != want {
		t.Fatalf("uv0 bias %v, want the minimum %v", record.UV0Bias, want)
	}
	// The second set is one point, so it is the zero-width case and its bias is
	// the constant.
	if record.UV1Scale != (m.Vec2{}) || record.UV1Bias != (m.Vec2{X: -1, Y: -1}) {
		t.Fatalf("uv1 scale %v bias %v, want zero and the constant", record.UV1Scale, record.UV1Bias)
	}
	packed := at.of(arena)
	for i, vertex := range vertices {
		stored := readStoredVertex(t, packed, i, storageStride, record)
		if stored.uv0 != vertex.UV0 || stored.uv1 != vertex.UV1 {
			t.Errorf("vertex %d stored %v %v, want the authored %v %v",
				i, stored.uv0, stored.uv1, vertex.UV0, vertex.UV1)
		}
	}
}

// uvTriangle is the smallest standard-layout mesh carrying a UV range worth a
// record: one face whose TEXCOORD_0 tiles well outside 0..1, which is the case
// a half float loses four texels of.
func uvTriangle() []Vertex {
	white := m.White
	return []Vertex{
		{Position: m.Vec3{X: -1, Y: -1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 2.5, Y: -13.5}, Color: white},
		{Position: m.Vec3{X: 1, Y: -1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 18.5, Y: 0.5}, Color: white},
		{Position: m.Vec3{Y: 1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 10, Y: -6}, Color: white},
	}
}

// boundRecords reads back what one binding was bound to, as the bytes the
// backend was handed. Everything downstream of the arena is an offset and a
// size, so this is the only place a record scene packed is observable.
func boundRecords(t *testing.T, h *harness, name string) []byte {
	t.Helper()
	bound := h.backend.buffersBoundTo(name)
	if len(bound) == 0 {
		t.Fatalf("%s was never bound; a declared binding left unbound takes the frame down", name)
	}
	data := h.backend.baked[bound[0].buffer]
	if bound[0].size == 0 {
		return data[bound[0].offset:]
	}
	return data[bound[0].offset : bound[0].offset+bound[0].size]
}

func readMeshRecord(data []byte, index int) sceneMesh {
	at := data[index*meshRecordSize:]
	read := func(offset int) m.Vec2 {
		return m.Vec2{X: readFloat32(at[offset:]), Y: readFloat32(at[offset+4:])}
	}
	return sceneMesh{UV0Scale: read(0), UV0Bias: read(8), UV1Scale: read(16), UV1Bias: read(24)}
}

func readInstanceMesh(data []byte, index int) uint32 {
	var instance sceneInstance
	return binary.NativeEndian.Uint32(data[index*instanceSize+int(unsafe.Offsetof(instance.Mesh)):])
}

// The per-mesh buffer is bound on every draw, slot 0 holds the identity, and a
// mesh with a range of its own gets a slot after it which its instance names.
func TestAMeshWithAUVRangeNamesItsOwnSlotAfterTheIdentity(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(uvTriangle(), nil, gfx.TopologyTriangleList)
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
	})
	h.frame()

	meshes := boundRecords(t, h, "sceneMeshes")
	if len(meshes) != 2*meshRecordSize {
		t.Fatalf("the per-mesh buffer holds %d bytes, want the identity and one record at %d each",
			len(meshes), meshRecordSize)
	}
	if slot0 := readMeshRecord(meshes, 0); slot0 != identityMesh {
		t.Fatalf("slot 0 is %+v, want the identity %+v", slot0, identityMesh)
	}
	_, _, want := packVertices(new([]byte), uvTriangle())
	if got := readMeshRecord(meshes, 1); got != want {
		t.Fatalf("slot 1 is %+v, want the range the pack derived %+v", got, want)
	}
	if named := readInstanceMesh(boundRecords(t, h, "sceneInstances"), 0); named != 1 {
		t.Fatalf("the instance names slot %d, want the mesh's own slot 1", named)
	}
}

// A custom-layout mesh has no range scene could derive - it cannot find a UV
// inside a struct it does not know - so it names slot 0 and the buffer stays
// one record long.
func TestACustomLayoutMeshNamesTheIdentitySlot(t *testing.T) {
	custom := []customVertex{{Position: m.Vec3{X: -1, Y: -1}}, {Position: m.Vec3{X: 1, Y: -1}}, {}}
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(custom, nil, gfx.TopologyTriangleList)
		q.Mesh(0, ref, MeshDraw{Material: opaqueMaterial(7), NeverCull: true})
	})
	h.frame()

	meshes := boundRecords(t, h, "sceneMeshes")
	if len(meshes) != meshRecordSize {
		t.Fatalf("the per-mesh buffer holds %d bytes, want the identity alone", len(meshes))
	}
	if slot0 := readMeshRecord(meshes, 0); slot0 != identityMesh {
		t.Fatalf("slot 0 is %+v, want the identity %+v", slot0, identityMesh)
	}
	if named := readInstanceMesh(boundRecords(t, h, "sceneInstances"), 0); named != 0 {
		t.Fatalf("the custom-layout draw names slot %d, want the identity at 0", named)
	}
}
