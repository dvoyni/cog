package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

// skinPosition is deform.wgsl's influence loop written in Go, and it exists for
// the same reason octDecode does: nothing in this tree can run WGSL, so the
// shipping arithmetic is transcribed beside the encoder it has to agree with.
//
// posed stands in for skin * vec4(base, 1) - one joint's fully composed
// transform applied to the bind-pose position - because what is under test is
// the accumulation and the divide, not the matrix build. The weights arrive as
// codes and are divided by their maximum here, which is what the fetch unit
// does for a Unorm8x4 before the shader sees anything.
func skinPosition(joints, weights [4]byte, base m.Vec3, posed func(joint byte) m.Vec3) m.Vec3 {
	var position m.Vec3
	var total float32
	for influence := range 4 {
		weight := float32(weights[influence]) / unorm8CodeMax
		if weight == 0 {
			continue
		}
		total += weight
		position = position.Add(posed(joints[influence]).MulS(weight))
	}
	// A vertex with no influence at all keeps its bind pose rather than
	// collapsing to the origin, which is the guard the divide now shares.
	if total == 0 {
		return base
	}
	return position.DivS(total)
}

// exactSkinPosition is the same blend at the authored float weights: what the
// stored codes are an approximation of, and the answer both of the tests below
// measure against.
func exactSkinPosition(joints [4]byte, weights m.Vec4, posed func(joint byte) m.Vec3) m.Vec3 {
	var position m.Vec3
	for influence, weight := range [4]float32{weights.X, weights.Y, weights.Z, weights.W} {
		position = position.Add(posed(joints[influence]).MulS(weight))
	}
	return position
}

// twoBones places joint 0 at the origin and joint 1 ten units along x, so a
// blend between them is a point on a segment whose position reads off the
// weights directly and whose error is visible as a distance.
func twoBones(joint byte) m.Vec3 {
	if joint == 0 {
		return m.Vec3{}
	}
	return m.Vec3{X: 10}
}

// glTF only says a producer SHOULD normalise WEIGHTS_0, so a file whose weights
// sum to something other than one is malformed but legal - and today's skin
// path trusts the sum and shrinks or inflates the mesh by exactly that factor
// with nothing reported. The divide is what makes such a file skinned correctly
// instead.
//
// Half-weight and double-weight are the two directions, and the assertion is
// that both land on the same point as the normalised set: the ratio between the
// influences is the only thing a weight vector was ever saying.
func TestWeightsThatDoNotSumToOneSkinToTheSamePointAsOnesThatDo(t *testing.T) {
	joints := [4]byte{0, 1, 0, 0}
	want := m.Vec3{X: 7.5}
	for _, c := range []struct {
		what    string
		weights [4]byte
	}{
		{"a quarter and three quarters", [4]byte{64, 191, 0, 0}},
		{"half of that, summing to 0.5", [4]byte{32, 96, 0, 0}},
		{"a third more, summing to 4/3", [4]byte{85, 255, 0, 0}},
		{"a hundredth of it", [4]byte{1, 3, 0, 0}},
	} {
		got := skinPosition(joints, c.weights, m.Vec3{X: -1}, twoBones)
		if m.Vec3(got).Distance(want) > 0.02 {
			t.Errorf("%s skins to %v, want the three-quarter point %v", c.what, got, want)
		}
	}
	// The one case the divide must not touch: no influence at all is a
	// malformed vertex rather than a case to be correct about, and dividing by
	// a zero total would spread a NaN through the whole fragment stage.
	base := m.Vec3{X: 3, Y: 4, Z: 5}
	if got := skinPosition(joints, [4]byte{}, base, twoBones); got != base {
		t.Errorf("a vertex with no influence skins to %v, want its bind pose %v", got, base)
	}
}

// fourBones puts the blended vertex a long way from the origin with the bones
// close together, which is the shape real content has: Fox's z extent is 155
// units and its 24 bones sit inside the animal. It is that ratio that decides
// what an unnormalised sum costs, because dropping the divide scales the whole
// deformed position about the origin.
func fourBones(joint byte) m.Vec3 {
	return m.Vec3{X: 100 + 10*float32(joint)}
}

// The eight-bit weight is what makes the divide load-bearing for ordinary,
// well-formed content too - not only for a malformed file. Four weights that
// sum to exactly one in the file round to codes that sum to 255 only some of
// the time: 11.7% of Fox's vertices land one code off, and Fox's file weights
// are normalised to within a float32 ULP, so every bit of that drift is
// scene's own doing.
//
// Both halves are measured, because a test that only pinned the good number
// would pass with the divide deleted: that the drift is real at the codes, and
// that the divide is what removes it. The bound on the divided answer is the
// error quantising the ratio can leave once the sum is out of the way, which is
// bounded by the spread of the bones rather than by the distance to the origin;
// the undivided answer carries that plus the whole position scaled by a sum
// that is not one.
func TestTheDivideRemovesTheDriftEightBitWeightsPutIntoANormalisedSet(t *testing.T) {
	joints := [4]byte{0, 1, 2, 3}
	var drifted, samples int
	var worstDivided, worstRaw float64
	// A deterministic sweep over the simplex: every four-way split at a step
	// far finer than a code, so the rounding is exercised on both sides of
	// every boundary and a failure is reproducible.
	for a := 1; a <= 37; a++ {
		for b := 1; b <= 37; b++ {
			for c := 1; c <= 37; c++ {
				for d := 1; d <= 37; d++ {
					total := float32(a + b + c + d)
					weights := m.Vec4{
						X: float32(a) / total, Y: float32(b) / total,
						Z: float32(c) / total, W: float32(d) / total,
					}
					codes := [4]byte{
						packWeight(weights.X), packWeight(weights.Y),
						packWeight(weights.Z), packWeight(weights.W),
					}
					samples++
					if int(codes[0])+int(codes[1])+int(codes[2])+int(codes[3]) != unorm8CodeMax {
						drifted++
					}
					want := exactSkinPosition(joints, weights, fourBones)
					divided := skinPosition(joints, codes, m.Vec3{}, fourBones)
					worstDivided = math.Max(worstDivided, float64(divided.Distance(want)))

					// The same blend with the divide taken out, which is what
					// the skin path did before this: it accumulated the total
					// for the zero-influence check and then threw it away.
					var raw m.Vec3
					for influence := range 4 {
						raw = raw.Add(fourBones(joints[influence]).
							MulS(float32(codes[influence]) / unorm8CodeMax))
					}
					worstRaw = math.Max(worstRaw, float64(raw.Distance(want)))
				}
			}
		}
	}
	if drifted == 0 {
		t.Fatal("no sample's codes missed 255, so this measures nothing")
	}
	// Two codes of the 30 units the four bones span, which is what the ratio
	// alone can cost. The distance to the origin does not appear in it, and
	// that is the whole difference the divide makes.
	if bound := 2 * 30.0 / unorm8CodeMax; worstDivided > bound {
		t.Errorf("the divided blend is %.6f out at worst, want under %.6f", worstDivided, bound)
	}
	if worstRaw < 2*worstDivided {
		t.Errorf("dropping the divide costs %.6f against %.6f, so the divide is doing nothing",
			worstRaw, worstDivided)
	}
	t.Logf("%d of %d code sets miss 255; worst %.6f divided against %.6f raw",
		drifted, samples, worstDivided, worstRaw)
}

// A joint index the vertex cannot hold saturates rather than wrapping. Both
// paths that reach the packer guarantee it never happens - the load rejects a
// model whose skins claim more than the cap, and an authored mesh never skins -
// and what this pins is that if either guarantee is ever lost, the failure is a
// bone at the end of the range rather than an arbitrary one in the middle.
func TestAJointPastTheCapSaturatesRatherThanWrapping(t *testing.T) {
	for _, c := range []struct {
		joint uint16
		want  byte
	}{{0, 0}, {23, 23}, {255, 255}, {256, 255}, {300, 255}, {65535, 255}} {
		if got := packJoint(c.joint); got != c.want {
			t.Errorf("joint %d packs to %d, want %d", c.joint, got, c.want)
		}
	}
}

// The weight code is a plain unorm8 with nothing clever in it, and the ends are
// where a rounding scheme goes wrong: zero has to stay zero so the skin path's
// zero-weight continue still fires, and one has to reach 255 so a single full
// influence is not 254/255 of itself.
func TestTheWeightCodeIsAPlainUnormWithBothEndsExact(t *testing.T) {
	for _, c := range []struct {
		weight float32
		want   byte
	}{
		{0, 0}, {1, unorm8CodeMax}, {0.5, 128}, {1.0 / unorm8CodeMax, 1},
		{-0.25, 0}, {2, unorm8CodeMax}, {float32(math.NaN()), 0},
	} {
		if got := packWeight(c.weight); got != c.want {
			t.Errorf("weight %v packs to %d, want %d", c.weight, got, c.want)
		}
	}
}
