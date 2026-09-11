package scene

import (
	"math"

	"github.com/dvoyni/cog/m"
)

// The octahedral encode, which is how the normal and the tangent each fit in
// four bytes.
//
// A unit direction is projected onto the octahedron |x| + |y| + |z| = 1 and the
// lower half of that octahedron is folded out onto the square, so two numbers
// in [-1, 1] name a direction. Quantised to two 16-bit unorms it is 26x more
// accurate than the fetch unit's packed 10/10/10/2 at the same four bytes -
// 0.0037 degrees mean against 0.042 - and it needs no binding, no CPU-side
// table and nothing for an update path to keep in sync, because the decode is
// arithmetic on the stored word alone.
//
// The decode is not here. It runs on the GPU, published as WGSL at
// builtin/scene/vertexdecode.wgsl (VertexDecodePath) so an app writing its own
// scene material reads the same directions the bundled one does rather than a
// re-typed approximation. What is here is only the half that runs at bake, and
// scene/vertexoct_test.go holds the transcription that proves the two invert.
//
// Magnitude is discarded, silently, as contract: this divides by the L1 norm,
// so a direction of length two encodes exactly as the same direction of length
// one. Reporting a non-unit normal was considered and rejected - a normal
// computed from a cross product is one float of rounding from a spurious
// error, and a length-1.0001 normal draws correctly.

const (
	// octNormalMax is the largest code of the normal's 16-bit unorm pair, and
	// octTangentMax the largest of the tangent's 15-bit one. Each is the
	// divisor the fetch unit and the decode use, so the encode has to scale by
	// exactly it and not by a power of two beside it.
	octNormalMax  = 0xFFFF
	octTangentMax = 0x7FFF

	// The tangent's one 32-bit word: 15 bits of octahedral x, 15 of y, the
	// handedness above them and one bit reserved.
	//
	// Handedness rides in the vertex because a primitive is allowed to carry
	// both signs - WaterBottle's does - so one bit per primitive could not
	// express the corpus scene already loads. The reserved bit stays reserved
	// and is never written: it was the only place an absence sentinel could
	// have lived, and there is nothing to say absent about, since the loader
	// generates a missing tangent at load and no authoring consumer
	// distinguishes "no tangent" from "zero tangent".
	//
	// The word has roughly 14 bits of slack beyond that - the tangent's usable
	// floor is near 8 bits per axis - and the slack is unspendable: a two-byte
	// tangent gives a 30-byte stride and WebGPU requires arrayStride to be a
	// multiple of 4, so it pads straight back. What blocks a narrower tangent
	// is packing, not fidelity.
	octTangentYShift     = 15
	tangentHandednessBit = 1 << 30
	tangentReservedBit   = 1 << 31
)

// octEncode projects a direction onto the octahedron and unfolds it to the
// square, reporting the two components in [-1, 1] that name it.
func octEncode(direction m.Vec3) m.Vec2 {
	l1 := abs32(direction.X) + abs32(direction.Y) + abs32(direction.Z)
	// The one guard in the whole encoding, and the canonical for an unwritten
	// direction falls out of it rather than being chosen: a zero-length vector
	// has no projection, so it encodes as the octahedral origin, which decodes
	// to +Z. A NaN or infinite component fails this comparison the same way and
	// lands on the same answer, which is what keeps a NaN from reaching the
	// shader and spreading through the whole fragment stage.
	if !(l1 > 0) || math.IsInf(float64(l1), 1) {
		return m.Vec2{}
	}
	folded := m.Vec2{X: direction.X / l1, Y: direction.Y / l1}
	if direction.Z < 0 {
		folded = m.Vec2{
			X: (1 - abs32(folded.Y)) * signNotZero(folded.X),
			Y: (1 - abs32(folded.X)) * signNotZero(folded.Y),
		}
	}
	return folded
}

// signNotZero is the sign function the fold needs: zero is positive, so that
// the two halves of the octahedron partition the square rather than overlapping
// on its axes. The decode's select reads the same way.
func signNotZero(value float32) float32 {
	if value >= 0 {
		return 1
	}
	return -1
}

// quantizeUnorm maps one octahedral component in [-1, 1] onto its unorm code.
// The clamps are for the ends of the range only: an encoded component is inside
// it by construction, and float arithmetic is what can put it a bit outside.
func quantizeUnorm(value float32, maxCode uint32) uint32 {
	scaled := (float64(value)*0.5 + 0.5) * float64(maxCode)
	if !(scaled > 0) {
		return 0
	}
	if scaled >= float64(maxCode) {
		return maxCode
	}
	return uint32(scaled + 0.5)
}

// packNormal encodes one normal as the two 16-bit unorm codes the storage
// vertex holds at location 1.
func packNormal(normal m.Vec3) (uint16, uint16) {
	folded := octEncode(normal)
	return uint16(quantizeUnorm(folded.X, octNormalMax)), uint16(quantizeUnorm(folded.Y, octNormalMax))
}

// packTangent encodes one tangent as the single 32-bit word the storage vertex
// holds at location 2: 15 bits per octahedral axis, the handedness of w, and a
// reserved bit left clear.
//
// glTF's w is exactly +1 or -1 everywhere in the corpus, and an unwritten
// tangent's w is 0, which is not negative and therefore stores as +1 - the same
// answer the zero direction gives for the axis, so the whole zero vertex
// decodes to a right-handed +Z frame with nothing special-cased.
func packTangent(tangent m.Vec4) uint32 {
	folded := octEncode(m.Vec3{X: tangent.X, Y: tangent.Y, Z: tangent.Z})
	word := quantizeUnorm(folded.X, octTangentMax) |
		quantizeUnorm(folded.Y, octTangentMax)<<octTangentYShift
	if tangent.W >= 0 {
		word |= tangentHandednessBit
	}
	return word
}
