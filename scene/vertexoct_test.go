package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

// octDecode is the WGSL decode written in Go, and it exists because nothing in
// this tree can run WGSL: builtin/scene/vertexdecode.wgsl is the shipping
// decoder and this is a transcription of it, line for line, kept beside the
// encoder it has to invert.
//
// A drift between the two would be invisible here, which is why the WGSL is
// three lines of arithmetic with no branches to get wrong and why this function
// is not exported into the package: it is the test's own reference, not a
// second decoder scene offers anyone.
func octDecode(e m.Vec2) m.Vec3 {
	n := m.Vec3{X: e.X, Y: e.Y, Z: 1 - abs32(e.X) - abs32(e.Y)}
	fold := float32(math.Max(float64(-n.Z), 0))
	n.X -= fold * signNotZero(n.X)
	n.Y -= fold * signNotZero(n.Y)
	return n.Normalize()
}

// decodeStoredNormal and decodeStoredTangent take the codes as the fetch unit
// hands them to the shader - a unorm divided by its maximum - rather than the
// codes themselves, so the test travels the whole path the GPU does.
func decodeStoredNormal(x, y uint16) m.Vec3 {
	return octDecode(m.Vec2{
		X: float32(x)/octNormalMax*2 - 1,
		Y: float32(y)/octNormalMax*2 - 1,
	})
}

func decodeStoredTangent(word uint32) m.Vec4 {
	direction := octDecode(m.Vec2{
		X: float32(word&octTangentMax)/octTangentMax*2 - 1,
		Y: float32((word>>octTangentYShift)&octTangentMax)/octTangentMax*2 - 1,
	})
	handedness := float32(-1)
	if word&tangentHandednessBit != 0 {
		handedness = 1
	}
	return m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z, W: handedness}
}

// angleBetween reports the angle in degrees between two unit directions. It is
// the only error measure that means anything for a normal: the encode discards
// magnitude by contract, so a comparison of components would be measuring
// something scene does not store.
//
// The arithmetic is float64 even though both inputs are float32, because acos
// near one amplifies: a float32 dot product of two nearly equal directions is
// wrong by an ULP, and an ULP under the arccosine is 0.028 degrees - larger
// than any error this test is looking for, and a floor the measurement would
// otherwise report as the encoder's.
func angleBetween(a, b m.Vec3) float64 {
	ax, ay, az := normalize64(a)
	bx, by, bz := normalize64(b)
	dot := ax*bx + ay*by + az*bz
	return math.Acos(math.Min(1, math.Max(-1, dot))) * 180 / math.Pi
}

func normalize64(v m.Vec3) (float64, float64, float64) {
	x, y, z := float64(v.X), float64(v.Y), float64(v.Z)
	length := math.Sqrt(x*x + y*y + z*z)
	if length == 0 {
		return 0, 0, 0
	}
	return x / length, y / length, z / length
}

// sphereDirections is a Fibonacci sphere: count directions spread evenly over
// the whole sphere, deterministic, and dense enough to land in every octant and
// on every fold of the octahedron. A random sample would make a failure
// unreproducible for a bound that is meant to hold everywhere.
func sphereDirections(count int) []m.Vec3 {
	directions := make([]m.Vec3, count)
	golden := math.Pi * (3 - math.Sqrt(5))
	for i := range directions {
		z := 1 - 2*(float64(i)+0.5)/float64(count)
		radius := math.Sqrt(math.Max(0, 1-z*z))
		angle := golden * float64(i)
		directions[i] = m.Vec3{
			X: float32(radius * math.Cos(angle)),
			Y: float32(radius * math.Sin(angle)),
			Z: float32(z),
		}
	}
	return directions
}

// The four bytes are spent on accuracy, so the accuracy is what is pinned. The
// bound is oct32's published figure with room around it - 0.0037 degrees mean,
// against Unorm1010102's 0.042 at the same four bytes - and it is asserted over
// the whole sphere rather than over a sample of one octant, because the fold
// between octants is where an octahedral encoding goes wrong.
func TestTheStoredNormalHoldsEveryDirectionToAHundredthOfADegree(t *testing.T) {
	var worst, total float64
	directions := sphereDirections(20000)
	for _, direction := range directions {
		x, y := packNormal(direction)
		angle := angleBetween(direction, decodeStoredNormal(x, y))
		total += angle
		if angle > worst {
			worst = angle
		}
	}
	mean := total / float64(len(directions))
	if worst > 0.01 {
		t.Errorf("the worst normal is %.5f degrees out, want under 0.01", worst)
	}
	if mean > 0.005 {
		t.Errorf("the mean normal is %.5f degrees out, want under 0.005", mean)
	}
	t.Logf("oct32 normal: mean %.5f degrees, worst %.5f", mean, worst)
}

// The tangent drops a bit per axis to make room for handedness, which roughly
// doubles its angular error and leaves it an order of magnitude better than the
// fetch unit's packed 10/10/10/2 - and the fragment stage re-orthogonalises it
// against the normal anyway. Its measured floor is near 8 bits per axis, so
// this bound is nowhere near the useful one; what it guards is that the bits
// went where they were meant to.
func TestTheStoredTangentHoldsEveryDirectionToAFiftiethOfADegree(t *testing.T) {
	var worst, total float64
	directions := sphereDirections(20000)
	for _, direction := range directions {
		word := packTangent(m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z, W: 1})
		decoded := decodeStoredTangent(word)
		angle := angleBetween(direction, m.Vec3{X: decoded.X, Y: decoded.Y, Z: decoded.Z})
		total += angle
		if angle > worst {
			worst = angle
		}
	}
	mean := total / float64(len(directions))
	if worst > 0.02 {
		t.Errorf("the worst tangent is %.5f degrees out, want under 0.02", worst)
	}
	if mean > 0.01 {
		t.Errorf("the mean tangent is %.5f degrees out, want under 0.01", mean)
	}
	t.Logf("oct 15/15 tangent: mean %.5f degrees, worst %.5f", mean, worst)
}

// The canonical for an unwritten direction is not chosen, it falls out of the
// encoding: a zero-length vector has no octahedral projection, the encoder
// returns the origin, and the origin decodes to +Z. The tangent's handedness
// comes from w = 0, which is not negative, so it is +1.
//
// What this really guards is that nothing anywhere produces a NaN. A NaN normal
// spreads through the whole fragment stage and paints a black or missing
// surface with nothing reported, and the zero vertex is the one an author gets
// by leaving a field out.
func TestAnUnwrittenNormalOrTangentDecodesToPlusZWithPositiveHandedness(t *testing.T) {
	x, y := packNormal(m.Vec3{})
	normal := decodeStoredNormal(x, y)
	if angle := angleBetween(normal, m.Vec3{Z: 1}); angle > 0.01 {
		t.Errorf("an unwritten normal decodes to %v, %.5f degrees off +Z", normal, angle)
	}
	tangent := decodeStoredTangent(packTangent(m.Vec4{}))
	if angle := angleBetween(m.Vec3{X: tangent.X, Y: tangent.Y, Z: tangent.Z}, m.Vec3{Z: 1}); angle > 0.01 {
		t.Errorf("an unwritten tangent decodes to %v, %.5f degrees off +Z", tangent, angle)
	}
	if tangent.W != 1 {
		t.Errorf("an unwritten tangent has handedness %v, want +1", tangent.W)
	}
	// Every path into the encoder that has no direction to encode, including
	// the ones an author reaches by computing a normal that cancels out.
	for _, degenerate := range []m.Vec3{
		{}, {X: float32(math.NaN())}, {Y: float32(math.Inf(1))}, {Z: float32(math.Inf(-1))},
	} {
		x, y := packNormal(degenerate)
		decoded := decodeStoredNormal(x, y)
		if math.IsNaN(float64(decoded.X + decoded.Y + decoded.Z)) {
			t.Errorf("%v decodes to %v, which carries a NaN", degenerate, decoded)
		}
	}
}

// Handedness rides in the vertex because one primitive in the corpus carries
// both signs - WaterBottle's - and one counter-example is enough to kill a
// per-primitive bit. Two vertices of one mesh disagreeing about handedness is
// therefore an ordinary case, not a malformed one.
func TestHandednessSurvivesAPrimitiveCarryingBothSigns(t *testing.T) {
	direction := m.Vec3{X: 0.5, Y: -0.5, Z: 0.7071}.Normalize()
	right := packTangent(m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z, W: 1})
	left := packTangent(m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z, W: -1})
	if w := decodeStoredTangent(right).W; w != 1 {
		t.Errorf("a right-handed tangent decodes with handedness %v, want +1", w)
	}
	if w := decodeStoredTangent(left).W; w != -1 {
		t.Errorf("a left-handed tangent decodes with handedness %v, want -1", w)
	}
	// The same direction under either sign, so the only bit that may differ is
	// the handedness one - and the reserved bit is never one of them.
	if right^left != tangentHandednessBit {
		t.Errorf("the two signs differ in %#08x, want only the handedness bit %#08x",
			right^left, uint32(tangentHandednessBit))
	}
	for _, word := range []uint32{right, left, packTangent(m.Vec4{})} {
		if word&tangentReservedBit != 0 {
			t.Errorf("%#08x has the reserved bit set", word)
		}
	}
}
