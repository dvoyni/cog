package scene

import (
	"math"

	"github.com/dvoyni/cog/m"
)

// The morph delta store: how one primitive's targets become a block of raw
// words, narrowed to a quarter of their float width and holding records only
// for the vertices a target actually moves.
//
// Both axes at once, because either alone is not worth the change. Across the
// vendored corpus narrowing alone reaches 37.5% of the float layout and
// sparsity alone 12.9%; together they reach 4.8%, and 93.0% of the float store
// was exactly zero - 22,972 records of 24,704, with one primitive 100% zero:
// eight targets, 6 KiB, moving nothing. No file uses a glTF sparse accessor, so
// the zeros are dense in the source too; scene was not inflating them, it was
// inheriting them.
//
// A half float was predicted to win here - a delta is a displacement, so its
// magnitudes were expected to cluster near zero - and it loses. Half float's
// advantage is dynamic range and a target's deltas have none: the non-zero
// deltas fill their range, and the only small values are the zeros, which every
// candidate stores exactly and which the sparsity half stops storing at all. At
// the same eight bytes, f16 is 50x worse than 16-bit fixed point against a
// per-primitive range.
//
// The decode is the GPU's, in builtin/scene/morph.wgsl; what is here is the
// half that runs at load.

// The block one morphed primitive occupies in the model's word array, starting
// at morphBase:
//
//	ranges       3 words per present slot, one f32 per axis, bitcast
//	per target   3 words: base, first, count
//	records      target t holds count_t of them, dense over its span
//
// Vertex v of target t sits at base_t + (v - first_t) * recordWords when
// first_t <= v < first_t + count_t, and costs one unsigned compare otherwise.
// Runs and a per-record vertex index are both cheaper in bytes - 6.1 KiB and
// 3.5 KiB over the whole corpus - and both need a walk or a binary search
// inside the per-active-target loop, which is the innermost loop in the engine
// and the direction this path's own standard forbids.
const (
	// morphRangeWords is one slot's range: one f32 per axis. The three slots
	// take a width each rather than sharing one, because their ranges differ by
	// up to 60x on the same primitive and their errors differ in kind - a
	// position error is a displacement, a normal error is an angle.
	morphRangeWords = 3
	// morphTargetHeaderWords is one target's base, first and count. It is what
	// makes the per-target stride stop being a constant, and what frees the
	// sceneAnim header's targetStride word.
	morphTargetHeaderWords = 3
	// morphWordSize is the byte a block is counted in. The buffer is a raw word
	// array rather than an array of records, because the record is no longer a
	// whole number of vec4s and the block header is not a record at all.
	morphWordSize = 4
)

// The largest code each slot's fixed point reaches. They are the divisors
// WGSL's unpack2x16snorm and unpack4x8snorm apply, not the powers of two beside
// them, so a range end round-trips exactly.
const (
	morphPositionCodeMax  = 32767
	morphDirectionCodeMax = 127
)

// morphRanges is one primitive's quantisation ranges, indexed the way morphSlots
// is: for each slot, the largest magnitude any of its targets reaches on each
// axis.
//
// It is per primitive rather than per target because the block header carries
// one set and the shader reads it once per vertex. A target that moves less
// than its neighbours loses precision by the ratio, which is what a per-target
// range would buy back for three more words per target and a second load.
type morphRanges [len(morphSlots)]m.Vec3

// add widens one slot's range to hold a delta. The range is symmetric - the
// codes are signed - so it is the magnitude per axis that matters.
func (r *morphRanges) add(slot int, delta m.Vec4) {
	r[slot] = m.Vec3{
		X: max(r[slot].X, abs(delta.X)),
		Y: max(r[slot].Y, abs(delta.Y)),
		Z: max(r[slot].Z, abs(delta.Z)),
	}
}

// morphSpan is the run of vertices one target stores records for: the first it
// moves and how many records follow, dense within the span whether or not every
// one of them moves.
//
// A target that moves nothing is count 0. It keeps its slot rather than being
// dropped at load, because MorphWeights is positional over the flattened slot
// list and removing one silently renumbers every slot after it.
type morphSpan struct{ first, count int }

// morphLiveSpan finds one target's span over its dense records. Liveness is at
// record granularity rather than per slot: per-slot spans would recover the
// cross-slot waste - about 18% of live records - for triple the metadata and
// three compares in the innermost loop.
//
// The loader never reorders vertices to tighten a span. With eight targets no
// ordering makes all eight contiguous at once, so it would be a heuristic with
// no bound; it would rewrite the index buffer; and it collides with the unweld
// permutation, which already reorders for its own reasons. The scheme degrades
// to the float layout's density when locality is absent, which is a floor: it
// cannot lose, it can only fail to win.
func morphLiveSpan(records []m.Vec4, slots int) morphSpan {
	first, last := -1, -1
	for vertex := 0; (vertex+1)*slots <= len(records); vertex++ {
		live := false
		for _, slot := range records[vertex*slots : (vertex+1)*slots] {
			if slot != (m.Vec4{}) {
				live = true
				break
			}
		}
		if !live {
			continue
		}
		if first < 0 {
			first = vertex
		}
		last = vertex
	}
	if first < 0 {
		return morphSpan{}
	}
	return morphSpan{first: first, count: last - first + 1}
}

// packMorphBlock appends one primitive's block to the model's word array and
// returns it. The dense float deltas it reads are the load's own working copy,
// target-major with slots records per vertex, and are dropped after this.
//
// The per-target bases are absolute word indices rather than block-relative
// ones, so the shader's address is the header word plus one multiply-add with
// nothing else folded in. A block is therefore placed once, by this call, and
// the offset is what its primitives carry as morphBase.
func packMorphBlock(words []uint32, deltas []m.Vec4, mask morphMask, targets, vertexCount int) []uint32 {
	slots, recordWords := mask.slots(), mask.recordWords()
	if targets == 0 || slots == 0 || vertexCount == 0 {
		return words
	}
	targetStride := vertexCount * slots
	var ranges morphRanges
	spans := make([]morphSpan, targets)
	for target := range targets {
		records := deltas[target*targetStride : (target+1)*targetStride]
		spans[target] = morphLiveSpan(records, slots)
		for at, delta := range records {
			ranges.add(at%slots, delta)
		}
	}
	start := len(words)
	for slot := range morphSlots {
		if mask&morphSlots[slot].bit == 0 {
			continue
		}
		words = append(words,
			math.Float32bits(ranges[slot].X),
			math.Float32bits(ranges[slot].Y),
			math.Float32bits(ranges[slot].Z),
		)
	}
	at := start + slots*morphRangeWords + targets*morphTargetHeaderWords
	for target := range targets {
		words = append(words, uint32(at), uint32(spans[target].first), uint32(spans[target].count))
		at += spans[target].count * recordWords
	}
	for target := range targets {
		span := spans[target]
		base := target * targetStride
		for vertex := span.first; vertex < span.first+span.count; vertex++ {
			for slot := range morphSlots {
				if mask&morphSlots[slot].bit == 0 {
					continue
				}
				words = morphSlots[slot].pack(words, deltas[base+vertex*slots+slot], ranges[slot])
			}
		}
	}
	return words
}

// packMorphPosition writes one position delta as three 16-bit codes and 16
// reserved bits, in the two words unpack2x16snorm reads as a pair of pairs.
func packMorphPosition(words []uint32, delta m.Vec4, scale m.Vec3) []uint32 {
	x := morphSnormCode(delta.X, scale.X, morphPositionCodeMax)
	y := morphSnormCode(delta.Y, scale.Y, morphPositionCodeMax)
	z := morphSnormCode(delta.Z, scale.Z, morphPositionCodeMax)
	return append(words, uint32(uint16(x))|uint32(uint16(y))<<16, uint32(uint16(z)))
}

// packMorphDirection writes one normal or tangent delta as three 8-bit codes and
// 8 reserved bits, in the one word unpack4x8snorm reads.
//
// The tangent's fourth component is never stored: glTF carries every target
// accessor as a VEC3, tangent included, because handedness is not a thing a
// shape can move.
func packMorphDirection(words []uint32, delta m.Vec4, scale m.Vec3) []uint32 {
	x := morphSnormCode(delta.X, scale.X, morphDirectionCodeMax)
	y := morphSnormCode(delta.Y, scale.Y, morphDirectionCodeMax)
	z := morphSnormCode(delta.Z, scale.Z, morphDirectionCodeMax)
	return append(words, uint32(uint8(x))|uint32(uint8(y))<<8|uint32(uint8(z))<<16)
}

// morphSnormCode maps one delta component onto its fixed-point code against the
// slot's range.
//
// A zero-width range codes zero, which the decode's zero scale takes back to
// exactly zero. The guard is written as !(scale > 0) so that a NaN range - which
// finite deltas cannot produce, but which a NaN delta would - lands on 0 rather
// than on a division.
//
// The divide is float64 because the range is a magnitude the delta is measured
// against rather than a number near it: a target whose reach is 1 and whose
// smallest live delta is 1e-5 needs the ratio resolved, not the subtraction.
func morphSnormCode(value, scale float32, codeMax int) int32 {
	if !(scale > 0) {
		return 0
	}
	rounded := math.Round(float64(value) / float64(scale) * float64(codeMax))
	if rounded > float64(codeMax) {
		return int32(codeMax)
	}
	if rounded < float64(-codeMax) {
		return int32(-codeMax)
	}
	return int32(rounded)
}
