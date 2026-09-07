package scene

import (
	"math/bits"
	"slices"
	"unsafe"
)

// morphMask is which of position, normal and tangent one primitive's morph
// records carry, in that fixed slot order.
//
// The mask is per primitive rather than per target: per-target masks would make
// the stride vary within a block, so the shader could no longer compute a
// record's address with one multiply.
type morphMask uint32

const (
	morphPosition morphMask = 1 << iota
	morphNormal
	morphTangent
)

// slots is how many 16-byte slots one record spends. Records are vec4-aligned
// and masked, so the stride is 16 * popcount(mask): most targets in real assets
// carry POSITION only, and a fixed 48-byte record would store explicit zeros
// for the rest - a 10k-vertex face with 52 shapes is 25 MiB at 48 bytes and
// about 8 MiB when position-only targets cost 16.
func (mask morphMask) slots() int { return bits.OnesCount32(uint32(mask)) }

// prefix widens a mask to the contiguous run of slots ending at its highest
// one, so a gap is stored as explicit zeros rather than closed up.
//
// The shader is handed a stride and nothing else - the sceneAnim header carries
// morphStride, not a mask, and its layout is fixed - so which slots a record
// holds has to be recoverable from the stride alone, and that is true only when
// the mask is a prefix of position, normal, tangent. The gap this fills is a
// primitive whose targets carry a normal delta and no position delta, which
// costs 16 bytes per vertex per target of zeros. The alternatives were dropping
// the delta, which loses authored data, and spending a reserved header word on
// a mask, which every draw would then read for a case almost no file has.
func (mask morphMask) prefix() morphMask {
	if mask == 0 {
		return 0
	}
	return morphMask(1<<bits.Len32(uint32(mask))) - 1
}

// morphBinding is everything one primitive says about morphing: where its block
// of delta records sits in the model's one buffer, how that block is addressed,
// and which of the model's weight slots feed it.
//
// A slot belongs to a node, not a mesh. glTF requires every primitive of a mesh
// to carry the same targets in the same order, so a three-primitive
// eight-target mesh contributes eight slots rather than twenty-four - and
// node.weights overrides mesh.weights, so two nodes referencing the same mesh
// have independent weights while the deltas stay shared, byte-identical between
// them.
type morphBinding struct {
	// base is the vec4 index of this primitive's first record in
	// sceneMorphDeltas, and stride the vec4s one vertex spends in one target.
	base, stride uint32
	// targetStride is vertexCount * stride, folded here so the shader's delta
	// address is one multiply. No base-vertex correction is needed anywhere:
	// gfx.MeshDescr owns its buffers and binds them at offset 0, so
	// @builtin(vertex_index) is 0-based within a primitive.
	targetStride uint32
	// targets is how many targets the block holds, and slotBase where this
	// primitive's node's weight run starts in the model's flattened slot list.
	targets  int
	slotBase int
}

// morphed reports whether the primitive has anything to blend.
func (b morphBinding) morphed() bool { return b.targets > 0 }

// sceneMorphWeight is one active target as the shader reads it: its index
// inside the primitive's block and its blended weight.
//
// The list is count-prefixed and sparse rather than a dense 64-float block. The
// CPU knows which entries are non-zero before it writes anything, so a 52-shape
// face with five active shapes costs 40 bytes instead of 256, and the shader's
// zero-skip branch disappears entirely because zeros never reach the GPU.
//
// Two of these share one vec4 of the sceneAnim block, which is why the list is
// padded to a vec4 boundary rather than packed against the next block.
type sceneMorphWeight struct {
	Target uint32
	Weight float32
}

var morphWeightSize = int(unsafe.Sizeof(sceneMorphWeight{}))

// maxMorphTargets is how many targets one draw may blend at once. Stored
// targets are unlimited: with sparse packing the cap constrains neither memory
// nor layout, and is purely a guard against runaway per-vertex ALU.
const maxMorphTargets = 64

// morphWeightTolerance is the magnitude below which a target is not blended at
// all. It is an absolute value because glTF does not clamp weights to [0, 1]
// and a negative weight is meaningful - a target driven the other way is a
// legal shape, and culling by w > 0 would silently drop it.
const morphWeightTolerance = 1e-5

// blendMorphWeights resolves one model draw's weight vector over the model's
// whole flattened slot list.
//
// Morphing is linear in the weights, so blending N plays' weight vectors here
// and applying the deltas once is exactly equal to morphing per play and
// blending the results. Unlike the pose case there is no approximation traded
// away, which is why the entire morph blend is CPU-side and the shader never
// sees a play.
//
// The precedence is the specification's: a non-nil override wins wholesale,
// then the animated result, then the rest row - which is itself node.weights
// over mesh.weights over zero, resolved at load.
func blendMorphWeights(
	anim *residentAnimation, path string, plays []scenePlayRecord,
	frames []weightFrames, override []float32, overridden bool,
	dst []float32, report reportOnce,
) []float32 {
	slots := anim.slotCount
	dst = grow(dst, slots)
	clear(dst)
	if slots == 0 {
		return dst
	}
	if overridden {
		// A short slice leaves the remaining targets at 0 and a long one
		// ignores the tail. Neither is an error: a caller animating the first
		// two shapes of a fifty-shape face should not have to carry the other
		// forty-eight zeros, and a caller whose array outlives a model edit
		// should not lose the model.
		if len(override) > slots {
			report(morphWeightsReportKey(path), ErrModelMorphWeightsOverLength{
				Model: path, Weights: len(override), Slots: slots,
			})
		}
		copy(dst, override)
		return dst
	}
	if len(plays) == 0 {
		copy(dst, anim.weights[:slots])
		return dst
	}
	// The two folded weights are the play record's own: the CPU already
	// resolved weight * (1 - frac) and weight * frac against the normalised
	// play weights, so the two-frame lerp and the weighted mean across plays
	// are one accumulation.
	for i := range plays {
		if i >= len(frames) {
			break
		}
		first, second := frames[i].row0, frames[i].row1
		if first < 0 || second < 0 {
			continue
		}
		for slot := range slots {
			dst[slot] += plays[i].W0*anim.weights[first+slot] +
				plays[i].W1*anim.weights[second+slot]
		}
	}
	return dst
}

// selectMorphTargets culls and caps one primitive's slice of the blended weight
// vector into the sparse list the shader reads, appending to dst.
//
// Culling by magnitude is what makes the list sparse, and the cap is what
// bounds the per-vertex loop. Over the cap the lightest are dropped rather than
// the last ones read, so which targets survive does not depend on the order the
// file listed them.
func selectMorphTargets(
	weights []float32, dst []sceneMorphWeight, path string, report reportOnce,
) []sceneMorphWeight {
	for target, weight := range weights {
		if abs(weight) < morphWeightTolerance {
			continue
		}
		dst = append(dst, sceneMorphWeight{Target: uint32(target), Weight: weight})
	}
	if len(dst) <= maxMorphTargets {
		return dst
	}
	report(morphTargetsReportKey(path), ErrModelMorphTargetsOverLimit{
		Model: path, Targets: len(dst), Limit: maxMorphTargets,
	})
	slices.SortStableFunc(dst, func(a, b sceneMorphWeight) int {
		switch {
		case abs(a.Weight) > abs(b.Weight):
			return -1
		case abs(a.Weight) < abs(b.Weight):
			return 1
		}
		return 0
	})
	return dst[:maxMorphTargets]
}

// The report-once keys the morph path fires under, in the same namespace as the
// animation path's: a draw whose weight array is too long should still report a
// missing clip.
func morphWeightsReportKey(path string) string { return "model:" + path + "#morphweights" }
func morphTargetsReportKey(path string) string { return "model:" + path + "#morphtargets" }
