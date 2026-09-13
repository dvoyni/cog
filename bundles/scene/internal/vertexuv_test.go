package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
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
	if record != (SceneMesh{}) {
		t.Fatalf("a UV-less mesh derived %+v, want the empty record that names slot 0", record)
	}
	if got := roundTripUV(m.Vec2{}, IdentityMesh.UV0Scale, IdentityMesh.UV0Bias); got != (m.Vec2{}) {
		t.Fatalf("slot 0 decoded a zero UV to %v, want the origin", got)
	}
	// A mesh scene never packed - a custom layout - has the same empty record,
	// because nothing ever wrote one.
	if (MeshRecord{}).UV != (SceneMesh{}) {
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
	at, _, record := PackVertices(&arena, vertices)
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
	packed := at.Of(arena)
	for i, vertex := range vertices {
		stored := readStoredVertex(t, packed, i, StorageStride, record)
		if stored.uv0 != vertex.UV0 || stored.uv1 != vertex.UV1 {
			t.Errorf("vertex %d stored %v %v, want the authored %v %v",
				i, stored.uv0, stored.uv1, vertex.UV0, vertex.UV1)
		}
	}
}
