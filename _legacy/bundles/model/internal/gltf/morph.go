package gltf

import (
	"encoding/json"

	"github.com/qmuntal/gltf"
)

// readMorphTargets reads one primitive's morph targets, as float deltas indexed
// by the base primitive's vertices.
//
// Only what the base primitive authored is read. The model generates flat
// normals for a primitive that has none and tangents for one whose material
// needs them: those are its own reconstruction, not the asset's, so a NORMAL
// delta on a primitive with no authored NORMAL is dropped here rather than
// added to a normal the file never wrote. glTF also ignores a primitive's
// TANGENT when it carries no NORMAL, so an authored tangent needs an authored
// normal behind it.
func readMorphTargets(doc *gltf.Document, primitive *gltf.Primitive, geometry *Geometry) []MorphTarget {
	if len(primitive.Targets) == 0 || len(geometry.Positions) == 0 {
		return nil
	}
	hasNormal := geometry.NormalNamed
	hasTangent := hasNormal && geometry.TangentNamed
	targets := make([]MorphTarget, len(primitive.Targets))
	for i, attributes := range primitive.Targets {
		target := &targets[i]
		target.Position, target.PositionNamed = readDelta(doc, attributes, gltf.POSITION, true)
		target.Normal, target.NormalNamed = readDelta(doc, attributes, gltf.NORMAL, hasNormal)
		target.Tangent, target.TangentNamed = readDelta(doc, attributes, gltf.TANGENT, hasTangent)
	}
	return targets
}

// readDelta reads one attribute of one target when the base authored it. A
// target accessor that cannot be read is a malformed file; the attribute is
// still named, so it keeps its place in the record and reads as zero, which
// beats losing the shape entirely.
func readDelta(
	doc *gltf.Document, attributes gltf.PrimitiveAttributes, name string, authored bool,
) ([][3]float32, bool) {
	if !authored {
		return nil, false
	}
	if _, named := attributes[name]; !named {
		return nil, false
	}
	accessor, ok := attributeAccessor(doc, attributes, name)
	if !ok {
		return nil, true
	}
	deltas, err := readVec3(doc, accessor)
	if err != nil {
		return nil, true
	}
	return deltas, true
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
func (c *decoder) claimMorphSlots(node int) morphSlotRun {
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
	run = morphSlotRun{base: len(c.model.MorphDefaults), count: targets}
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
		c.model.MorphDefaults = append(c.model.MorphDefaults, weight)
		c.model.MorphNames = append(c.model.MorphNames, names[target])
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

// WeightCurve is one weights sampler decoded once. It is not a Curve because a
// weights channel stores Count scalars per keyframe rather than one value
// widened to four, so the keyframe stride is the mesh's rather than the
// format's.
type WeightCurve struct {
	Times  []float32
	Values []float32
	// Count is the scalars one keyframe holds, which is the targeted node's
	// target count as the file meant it.
	Count         int
	Interpolation Interpolation
}

// End reports the curve's last keyframe time, which is what a clip's duration
// is the maximum of.
func (c *WeightCurve) End() float32 {
	if len(c.Times) == 0 {
		return 0
	}
	return c.Times[len(c.Times)-1]
}

// weightCurve decodes one weights sampler, or returns nil for one that cannot
// be read. Samplers are interned per document the way the TRS curves are.
func (c *decoder) weightCurve(animation *gltf.Animation, index int) *WeightCurve {
	if index < 0 || index >= len(animation.Samplers) || animation.Samplers[index] == nil {
		return nil
	}
	key := samplerKey{animation: animation, sampler: index}
	if curve, ok := c.weightCurves[key]; ok {
		return curve
	}
	c.weightCurves[key] = nil
	sampler := animation.Samplers[index]
	input, ok := accessorAt(c.doc, &sampler.Input)
	if !ok {
		return nil
	}
	output, ok := accessorAt(c.doc, &sampler.Output)
	if !ok {
		return nil
	}
	curve := &WeightCurve{Interpolation: interpolationOf(sampler.Interpolation)}
	if err := readAttribute(c.doc, input, func(_ int, value attrValue) {
		curve.Times = append(curve.Times, value[0])
	}); err != nil {
		return nil
	}
	if err := readAttribute(c.doc, output, func(_ int, value attrValue) {
		curve.Values = append(curve.Values, value[0])
	}); err != nil {
		return nil
	}
	// The output is targetCount scalars per keyframe, and CUBICSPLINE stores
	// each keyframe as in-tangent, value, out-tangent - so the count the file
	// meant is what divides out here rather than something read from the mesh.
	keys := len(curve.Times)
	if curve.Interpolation == InterpolationCubicSpline {
		keys *= 3
	}
	if keys == 0 || len(curve.Values) < keys {
		return nil
	}
	curve.Count = len(curve.Values) / keys
	if curve.Count == 0 {
		return nil
	}
	c.weightCurves[key] = curve
	return curve
}

// weightsChannel resolves one weights channel against the model's flattened
// slot list. A channel targeting a node no scene reaches has no slots to write
// and is dropped: there is nothing addressable behind it.
func (c *decoder) weightsChannel(
	animation *gltf.Animation, channel *gltf.AnimationChannel, node int,
) (ClipWeights, bool) {
	run, ok := c.morphNodes[node]
	if !ok || run.count == 0 {
		return ClipWeights{}, false
	}
	curve := c.weightCurve(animation, channel.Sampler)
	if curve == nil {
		return ClipWeights{}, false
	}
	return ClipWeights{SlotBase: run.base, SlotCount: run.count, Curve: curve}, true
}
