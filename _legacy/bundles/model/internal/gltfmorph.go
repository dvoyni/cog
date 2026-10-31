package internal

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// gltfMorph is one primitive's morph targets converted: the mask every target
// in the block shares, the load's dense working copy of the deltas, and the
// reach they add to the primitive's bounds.
//
// The working copy is float and dense - every vertex of target 0, then every
// vertex of target 1 - because that is the shape the unweld permutation
// rewrites and the range accumulation reads. What reaches the GPU is neither:
// packMorphBlock narrows it and drops the 93% of records that are exactly zero.
type gltfMorph struct {
	targets int
	mask    morphMask
	// slots is m.Vec4s per vertex per target in the working copy, which is the
	// mask's slot count. The packed record's own stride is mask.recordWords().
	slots int
	// deltas is targets * vertexCount * slots records. It is packed into the
	// model's one buffer at pack time and nil afterwards.
	deltas []m.Vec4
	// reach is the largest position-delta magnitude summed over the targets:
	// conservative, because it assumes every target at weight 1 at once, and
	// computed in the one pass over delta data already being read.
	reach float32
	// base is the word index of this block's header in the model's one delta
	// buffer, assigned when the blocks are concatenated.
	base int
}

// morphed reports whether the primitive carries any target at all.
func (g gltfMorph) morphed() bool { return g.targets > 0 && g.slots > 0 }

// vertexCount recovers the primitive's vertex count from the working copy,
// which is the one place it is still written down after the conversion returns.
func (g gltfMorph) vertexCount() int {
	if !g.morphed() {
		return 0
	}
	return len(g.deltas) / g.targets / g.slots
}

// convertMorphTargets converts one decoded primitive's morph targets.
//
// The mask is the union across the primitive's targets of the attributes they
// name, widened to a prefix. The decoder has already dropped what the base
// primitive did not author: scene generates flat normals for a primitive that
// has none and tangents for one whose material needs them, and those are
// scene's own reconstruction, not the asset's.
func convertMorphTargets(decoded *DecodedGeometry, vertexCount int) gltfMorph {
	if len(decoded.Targets) == 0 || vertexCount == 0 {
		return gltfMorph{}
	}
	var mask morphMask
	for i := range decoded.Targets {
		for _, slot := range morphSlots {
			if _, named := slot.deltas(&decoded.Targets[i]); named {
				mask |= slot.bit
			}
		}
	}
	mask = mask.prefix()
	if mask == 0 {
		return gltfMorph{}
	}
	morph := gltfMorph{
		targets: len(decoded.Targets),
		mask:    mask,
		slots:   mask.slots(),
	}
	morph.deltas = make([]m.Vec4, morph.targets*vertexCount*morph.slots)
	targetStride := vertexCount * morph.slots
	for target := range decoded.Targets {
		at := 0
		for _, slot := range morphSlots {
			if mask&slot.bit == 0 {
				continue
			}
			offset := target*targetStride + at
			at++
			deltas, _ := slot.deltas(&decoded.Targets[target])
			// A target accessor whose count disagrees with the primitive's is
			// a malformed file. The vertices it does cover still morph, which
			// beats losing the shape entirely.
			for i, delta := range deltas[:min(len(deltas), vertexCount)] {
				morph.deltas[offset+i*morph.slots] = m.Vec4{X: delta[0], Y: delta[1], Z: delta[2]}
			}
		}
		if mask&morphPosition != 0 {
			morph.reach += maxLength(morph.deltas[target*targetStride:(target+1)*targetStride], morph.slots)
		}
	}
	return morph
}

// morphSlots is the fixed slot order a record holds its deltas in, the decoded
// target array each reads from, and the words and encoder each spends. It is
// fixed rather than derived so that the mask, the stride and the shader's slot
// offsets all agree without anything having to be transmitted.
//
// The widths are 2 / 1 / 1 words, so the prefix sums are 2 / 3 / 4 - distinct,
// which is what keeps "which slots a record holds is recoverable from the
// stride alone" true of a per-slot width. Each slot's offset inside a record
// stays a compile-time constant.
var morphSlots = [...]struct {
	bit    morphMask
	deltas func(target *DecodedMorphTarget) ([][3]float32, bool)
	words  int
	pack   func(words []uint32, delta m.Vec4, scale m.Vec3) []uint32
}{
	{bit: morphPosition, deltas: positionDeltas, words: 2, pack: packMorphPosition},
	{bit: morphNormal, deltas: normalDeltas, words: 1, pack: packMorphDirection},
	{bit: morphTangent, deltas: tangentDeltas, words: 1, pack: packMorphDirection},
}

func positionDeltas(target *DecodedMorphTarget) ([][3]float32, bool) {
	return target.Position, target.PositionNamed
}

func normalDeltas(target *DecodedMorphTarget) ([][3]float32, bool) {
	return target.Normal, target.NormalNamed
}

func tangentDeltas(target *DecodedMorphTarget) ([][3]float32, bool) {
	return target.Tangent, target.TangentNamed
}

// maxLength reports the largest magnitude among one target's position deltas,
// which sit at slot 0 of every vertex's run of slots.
func maxLength(records []m.Vec4, slots int) float32 {
	var longest float32
	for at := 0; at < len(records); at += slots {
		record := records[at]
		length := m.Vec3{X: record.X, Y: record.Y, Z: record.Z}.LengthSquared()
		longest = max(longest, length)
	}
	if longest == 0 {
		return 0
	}
	return float32(math.Sqrt(float64(longest)))
}

// remap rewrites the deltas through the permutation unwelding applied to the
// vertices, so a record still sits at its own vertex's index.
//
// Only a primitive that authored no NORMAL is unwelded, and such a primitive's
// mask is position-only - but the positions still have to follow their
// vertices, or a morphed face would pull the wrong corners.
func (g *gltfMorph) remap(source []uint32) {
	if !g.morphed() || len(source) == 0 {
		return
	}
	was := g.vertexCount()
	targetStride := len(source) * g.slots
	remapped := make([]m.Vec4, g.targets*targetStride)
	for target := range g.targets {
		for i, from := range source {
			if int(from) >= was {
				continue
			}
			at := target*targetStride + i*g.slots
			old := target*was*g.slots + int(from)*g.slots
			copy(remapped[at:at+g.slots], g.deltas[old:old+g.slots])
		}
	}
	g.deltas = remapped
}

// binding is the primitive's half of what a draw needs to address its block:
// the record stride and the target count, which conversion settles. The node's
// weight slots come from the flattening and the base from the concatenation,
// neither of which is known while a primitive is being placed.
func (g gltfMorph) binding() MorphBinding {
	if !g.morphed() {
		return MorphBinding{}
	}
	return MorphBinding{Stride: uint32(g.mask.recordWords()), Targets: g.targets}
}

// packMorphDeltas narrows every morphed primitive's targets into the model's
// one delta buffer and records where each block landed.
//
// One buffer per model rather than one per primitive: a buffer per primitive
// would mean a bind group per primitive, collapsing group 2's whole reason for
// existing.
//
// It runs after every scene is flattened, because a block's place in the buffer
// is not knowable until every primitive the file uses has been converted - so
// the flattened primitives take their base from here rather than carrying one
// the walk could only have guessed at.
func (c *modelConverter) packMorphDeltas() {
	for i := range c.model.geometries {
		morph := &c.model.geometries[i].morph
		if !morph.morphed() {
			continue
		}
		morph.base = len(c.model.morphDeltas)
		c.model.morphDeltas = packMorphBlock(
			c.model.morphDeltas, morph.deltas, morph.mask, morph.targets, morph.vertexCount(),
		)
		// The load's float working copy is dropped: the packed block is the
		// only reader from here on, and keeping both would hold a morphed
		// model's peak delta memory at the width this change exists to leave.
		morph.deltas = nil
	}
	for i := range c.model.primitives {
		primitive := &c.model.primitives[i]
		if primitive.morph.Morphed() {
			primitive.morph.Base = uint32(c.model.geometries[primitive.geometry].morph.base)
		}
	}
}

// morphCurve is one decoded weights sampler as the bake samples it. It is not
// an animCurve because a weights channel stores count scalars per keyframe
// rather than one value widened to four, so the keyframe stride is the mesh's
// rather than the format's.
type morphCurve struct {
	times  []float32
	values []float32
	// count is the scalars one keyframe holds, which is the targeted node's
	// target count.
	count int
	mode  DecodedInterpolation
}

// morphCurveOf wraps one decoded weights curve. It copies no keyframe.
func morphCurveOf(curve *DecodedWeightCurve) *morphCurve {
	return &morphCurve{
		times: curve.Times, values: curve.Values, count: curve.Count, mode: curve.Interpolation,
	}
}

// honouring the sampler's own interpolation.
//
// The bake is what STEP and CUBICSPLINE cost: they are evaluated here, at the
// source's own semantics, and the 60 Hz grid stores the result.
func (c *morphCurve) sample(time float32, dst []float32) {
	last := len(c.times) - 1
	if last < 0 {
		return
	}
	if time <= c.times[0] {
		c.copyKey(0, dst)
		return
	}
	if time >= c.times[last] {
		c.copyKey(last, dst)
		return
	}
	low, high := 0, last
	for low+1 < high {
		mid := (low + high) / 2
		if c.times[mid] <= time {
			low = mid
		} else {
			high = mid
		}
	}
	span := c.times[low+1] - c.times[low]
	if span <= 0 {
		c.copyKey(low, dst)
		return
	}
	amount := (time - c.times[low]) / span
	if c.mode == DecodedInterpolationStep {
		c.copyKey(low, dst)
		return
	}
	for target := range min(len(dst), c.count) {
		start, end := c.valueAt(low, target), c.valueAt(low+1, target)
		if c.mode == DecodedInterpolationCubicSpline {
			dst[target] = hermite(
				start, end,
				c.valueAt3(low*3+2, target), c.valueAt3((low+1)*3, target),
				amount, span,
			)
			continue
		}
		dst[target] = start + (end-start)*amount
	}
}

// copyKey writes one keyframe's values, which is what STEP and both ends of the
// curve read.
func (c *morphCurve) copyKey(key int, dst []float32) {
	for target := range min(len(dst), c.count) {
		dst[target] = c.valueAt(key, target)
	}
}

// valueAt reads one target's value at one keyframe, taking CUBICSPLINE's
// three-per-key layout into account: the value sits between its in and out
// tangents.
func (c *morphCurve) valueAt(key, target int) float32 {
	index := key
	if c.mode == DecodedInterpolationCubicSpline {
		index = key*3 + 1
	}
	return c.valueAt3(index, target)
}

// valueAt3 reads one scalar of the flat output array by its keyframe slot,
// which is what the tangent reads address directly.
func (c *morphCurve) valueAt3(slot, target int) float32 {
	at := slot*c.count + target
	if at < 0 || at >= len(c.values) {
		return 0
	}
	return c.values[at]
}

// hermite evaluates one CUBICSPLINE segment of a scalar channel. The tangents
// are stored per unit of the segment's own parameter, so both are scaled by the
// segment's length in seconds, which is glTF's own formulation.
func hermite(start, end, outgoing, incoming, amount, span float32) float32 {
	square := amount * amount
	cube := square * amount
	return start*(2*cube-3*square+1) +
		outgoing*(cube-2*square+amount)*span +
		end*(-2*cube+3*square) +
		incoming*(cube-square)*span
}

// bakeMorphWeights samples every clip's weights channels onto the same 60 Hz
// grid the poses use, as a plain []float32 that never reaches the GPU.
//
// Every row starts at the model's authored defaults and each clip overwrites
// only the slots it steers, which is the morph half of the rule the TRS bake
// follows: a clip that moves one shape leaves the other fifty-one where the
// artist left them, and the rest row is the defaults untouched.
func (c *modelConverter) bakeMorphWeights(
	animation *bakedAnimation, rows int, tracks []clipTrack,
) {
	slots := animation.slotCount
	if slots == 0 {
		return
	}
	animation.weights = make([]float32, rows*slots)
	for row := 0; row < len(animation.weights); row += slots {
		copy(animation.weights[row:row+slots], c.decoded.MorphDefaults)
	}
	rate := float32(c.sampleRate)
	for i := range tracks {
		track := &tracks[i]
		for frame := range track.clip.Frames {
			row := track.clip.WeightBase + frame*slots
			time := float32(frame) / rate
			for _, steered := range track.weights {
				at := row + steered.base
				steered.curve.sample(time, animation.weights[at:at+steered.count])
			}
		}
	}
}
