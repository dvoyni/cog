package scene

import (
	"encoding/json"
	"math"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
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

// readMorphTargets converts one primitive's morph targets.
//
// The mask is the union across the primitive's targets, intersected with the
// base primitive's authored attributes and then widened to a prefix. The
// intersection matters because scene generates flat normals for a primitive
// that has none and tangents for one whose material needs them: those are
// scene's own reconstruction, not the asset's, so a NORMAL delta on a primitive
// with no authored NORMAL is dropped here rather than added to a normal the
// file never wrote.
func readMorphTargets(
	doc *gltf.Document, primitive *gltf.Primitive, vertexCount int,
) gltfMorph {
	if len(primitive.Targets) == 0 || vertexCount == 0 {
		return gltfMorph{}
	}
	// glTF ignores a primitive's TANGENT when it carries no NORMAL - scene
	// unwelds and regenerates both in that case - so an authored tangent needs
	// an authored normal behind it.
	_, hasNormal := primitive.Attributes[gltf.NORMAL]
	_, hasTangent := primitive.Attributes[gltf.TANGENT]
	authored := morphPosition
	if hasNormal {
		authored |= morphNormal
		if hasTangent {
			authored |= morphTangent
		}
	}
	var mask morphMask
	for _, target := range primitive.Targets {
		for _, slot := range morphSlots {
			if _, named := target[slot.attribute]; named {
				mask |= slot.bit
			}
		}
	}
	mask = (mask & authored).prefix()
	if mask == 0 {
		return gltfMorph{}
	}
	morph := gltfMorph{
		targets: len(primitive.Targets),
		mask:    mask,
		slots:   mask.slots(),
	}
	morph.deltas = make([]m.Vec4, morph.targets*vertexCount*morph.slots)
	targetStride := vertexCount * morph.slots
	for target, attributes := range primitive.Targets {
		at := 0
		for _, slot := range morphSlots {
			if mask&slot.bit == 0 {
				continue
			}
			offset := target*targetStride + at
			at++
			accessor, ok := attributeAccessor(doc, attributes, slot.attribute)
			if !ok {
				continue
			}
			// A target accessor whose count disagrees with the primitive's is
			// a malformed file. The vertices it does cover still morph, which
			// beats losing the shape entirely.
			_ = readAttribute(doc, accessor, func(i int, value attrValue) {
				if i >= vertexCount {
					return
				}
				morph.deltas[offset+i*morph.slots] = m.Vec4{X: value[0], Y: value[1], Z: value[2]}
			})
		}
		if mask&morphPosition != 0 {
			morph.reach += maxLength(morph.deltas[target*targetStride:(target+1)*targetStride], morph.slots)
		}
	}
	return morph
}

// morphSlots is the fixed slot order a record holds its deltas in, the glTF
// attribute each reads from, and the words and encoder each spends. It is fixed
// rather than derived so that the mask, the stride and the shader's slot
// offsets all agree without anything having to be transmitted.
//
// The widths are 2 / 1 / 1 words, so the prefix sums are 2 / 3 / 4 - distinct,
// which is what keeps "which slots a record holds is recoverable from the
// stride alone" true of a per-slot width. Each slot's offset inside a record
// stays a compile-time constant.
var morphSlots = [...]struct {
	bit       morphMask
	attribute string
	words     int
	pack      func(words []uint32, delta m.Vec4, scale m.Vec3) []uint32
}{
	{bit: morphPosition, attribute: gltf.POSITION, words: 2, pack: packMorphPosition},
	{bit: morphNormal, attribute: gltf.NORMAL, words: 1, pack: packMorphDirection},
	{bit: morphTangent, attribute: gltf.TANGENT, words: 1, pack: packMorphDirection},
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
func (g gltfMorph) binding() morphBinding {
	if !g.morphed() {
		return morphBinding{}
	}
	return morphBinding{stride: uint32(g.mask.recordWords()), targets: g.targets}
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
		if primitive.morph.morphed() {
			primitive.morph.base = uint32(c.model.geometries[primitive.geometry].morph.base)
		}
	}
}

// claimMorphSlots gives one node the run of weight slots its mesh's targets
// need, allocating it the first time the flattening walk reaches that node.
//
// Slots are claimed per node rather than per mesh because node.weights
// overrides mesh.weights: two nodes referencing one head have independent
// weights and appear as two runs of the same names, while the deltas behind
// them stay shared and byte-identical.
//
// Depth-first, first-encounter order is what makes the flattened list the one
// MorphWeights indexes and MorphTargets reports.
func (c *modelConverter) claimMorphSlots(node int) morphSlotRun {
	if run, ok := c.morphNodes[node]; ok {
		return run
	}
	run := morphSlotRun{}
	mesh := c.doc.Nodes[node].Mesh
	if mesh == nil || *mesh < 0 || *mesh >= len(c.doc.Meshes) || c.doc.Meshes[*mesh] == nil {
		c.morphNodes[node] = run
		return run
	}
	targets := 0
	for _, primitive := range c.doc.Meshes[*mesh].Primitives {
		if primitive != nil {
			targets = max(targets, len(primitive.Targets))
		}
	}
	if targets == 0 {
		c.morphNodes[node] = run
		return run
	}
	run = morphSlotRun{base: len(c.morphDefaults), count: targets}
	// node.weights overrides mesh.weights, and a file that declares neither
	// starts every shape at zero, which is the identity for an additive blend.
	weights := c.doc.Nodes[node].Weights
	if len(weights) == 0 {
		weights = c.doc.Meshes[*mesh].Weights
	}
	names := morphTargetNames(c.doc.Meshes[*mesh], targets)
	for target := range targets {
		weight := float32(0)
		if target < len(weights) {
			weight = float32(weights[target])
		}
		c.morphDefaults = append(c.morphDefaults, weight)
		c.morphNames = append(c.morphNames, names[target])
	}
	c.morphNodes[node] = run
	return run
}

// morphSlotRun is one node's run of the model's flattened weight slots.
type morphSlotRun struct {
	base, count int
}

// morphTargetNames reads a mesh's target names, which glTF carries as the
// extras convention rather than as a member of its own: there is no other place
// in the format for them, and every exporter that names shapes writes them
// here.
//
// A mesh that names none, or names fewer than it has, leaves the rest empty
// rather than being skipped: the list is indexed by slot, not searched.
func morphTargetNames(mesh *gltf.Mesh, targets int) []string {
	names := make([]string, targets)
	var payload struct {
		TargetNames []string `json:"targetNames"`
	}
	switch extras := mesh.Extras.(type) {
	case map[string]any:
		raw, ok := extras["targetNames"].([]any)
		if !ok {
			return names
		}
		for i, entry := range raw {
			if i >= targets {
				break
			}
			if name, ok := entry.(string); ok {
				names[i] = name
			}
		}
		return names
	case json.RawMessage:
		if json.Unmarshal(extras, &payload) != nil {
			return names
		}
	case []byte:
		if json.Unmarshal(extras, &payload) != nil {
			return names
		}
	default:
		return names
	}
	copy(names, payload.TargetNames)
	return names
}

// morphCurve is one weights sampler decoded once. It is not an animCurve
// because a weights channel stores targetCount scalars per keyframe rather than
// one value widened to four, so the keyframe stride is the mesh's rather than
// the format's.
type morphCurve struct {
	times  []float32
	values []float32
	// count is the scalars one keyframe holds, which is the targeted node's
	// target count.
	count int
	mode  gltf.Interpolation
}

// end reports the curve's last keyframe time, which is what a clip's duration
// is the maximum of.
func (c *morphCurve) end() float32 {
	if len(c.times) == 0 {
		return 0
	}
	return c.times[len(c.times)-1]
}

// morphCurve decodes one weights sampler, or returns nil for one that cannot be
// read. Samplers are interned per document the way the TRS curves are.
func (c *modelConverter) morphCurve(animation *gltf.Animation, index int) *morphCurve {
	if index < 0 || index >= len(animation.Samplers) || animation.Samplers[index] == nil {
		return nil
	}
	key := samplerKey{animation: animation, sampler: index}
	if curve, ok := c.morphCurves[key]; ok {
		return curve
	}
	c.morphCurves[key] = nil
	sampler := animation.Samplers[index]
	input, ok := accessorAt(c.doc, &sampler.Input)
	if !ok {
		return nil
	}
	output, ok := accessorAt(c.doc, &sampler.Output)
	if !ok {
		return nil
	}
	curve := &morphCurve{mode: sampler.Interpolation}
	if err := readAttribute(c.doc, input, func(_ int, value attrValue) {
		curve.times = append(curve.times, value[0])
	}); err != nil {
		return nil
	}
	if err := readAttribute(c.doc, output, func(_ int, value attrValue) {
		curve.values = append(curve.values, value[0])
	}); err != nil {
		return nil
	}
	// The output is targetCount scalars per keyframe, and CUBICSPLINE stores
	// each keyframe as in-tangent, value, out-tangent - so the count the file
	// meant is what divides out here rather than something read from the mesh.
	keys := len(curve.times)
	if curve.mode == gltf.InterpolationCubicSpline {
		keys *= 3
	}
	if keys == 0 || len(curve.values) < keys {
		return nil
	}
	curve.count = len(curve.values) / keys
	if curve.count == 0 {
		return nil
	}
	c.morphCurves[key] = curve
	return curve
}

// sample evaluates the curve at a time into dst, one scalar per target,
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
	if c.mode == gltf.InterpolationStep {
		c.copyKey(low, dst)
		return
	}
	for target := range min(len(dst), c.count) {
		start, end := c.valueAt(low, target), c.valueAt(low+1, target)
		if c.mode == gltf.InterpolationCubicSpline {
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
	if c.mode == gltf.InterpolationCubicSpline {
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

// animatedWeights resolves one weights channel against the model's flattened
// slot list. A channel targeting a node no scene reaches has no slots to write
// and is dropped: there is nothing addressable behind it.
func (c *modelConverter) animatedWeights(
	animation *gltf.Animation, channel *gltf.AnimationChannel, node int,
) (animatedWeights, bool) {
	run, ok := c.morphNodes[node]
	if !ok || run.count == 0 {
		return animatedWeights{}, false
	}
	curve := c.morphCurve(animation, channel.Sampler)
	if curve == nil {
		return animatedWeights{}, false
	}
	return animatedWeights{run: run, curve: curve}, true
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
		copy(animation.weights[row:row+slots], c.morphDefaults)
	}
	rate := float32(c.sampleRate)
	for i := range tracks {
		track := &tracks[i]
		for frame := range track.clip.frames {
			row := track.clip.weightBase + frame*slots
			time := float32(frame) / rate
			for _, steered := range track.weights {
				at := row + steered.run.base
				steered.curve.sample(time, animation.weights[at:at+steered.run.count])
			}
		}
	}
}
