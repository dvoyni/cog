package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// morphedMesh adds one triangle mesh carrying morph targets and returns its
// index.
//
// The base attributes are written a slot at a time rather than through a
// helper, because which of them the file authored is exactly what decides the
// record mask - a fixture that always writes all three could not express the
// case the mask exists for.
func morphedMesh(
	doc *gltf.Document, normals, tangents bool, targets []gltf.PrimitiveAttributes,
) int {
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	attributes := gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)}
	if normals {
		attributes[gltf.NORMAL] = modeler.WriteNormal(doc, [][3]float32{
			{0, 0, 1}, {0, 0, 1}, {0, 0, 1},
		})
	}
	if tangents {
		attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{
			{1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1},
		})
	}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       "face",
		Primitives: []*gltf.Primitive{{Attributes: attributes, Targets: targets}},
	})
	return len(doc.Meshes) - 1
}

// deltaTarget writes one morph target's delta accessors. glTF stores every one
// of them as VEC3, tangent included - the handedness in w is not a thing a
// shape can move.
func deltaTarget(doc *gltf.Document, position, normal, tangent [][3]float32) gltf.PrimitiveAttributes {
	target := gltf.PrimitiveAttributes{}
	if position != nil {
		target[gltf.POSITION] = modeler.WriteAccessor(doc, gltf.TargetNone, position)
	}
	if normal != nil {
		target[gltf.NORMAL] = modeler.WriteAccessor(doc, gltf.TargetNone, normal)
	}
	if tangent != nil {
		target[gltf.TANGENT] = modeler.WriteAccessor(doc, gltf.TargetNone, tangent)
	}
	return target
}

// The mask is the union across the primitive's targets, intersected with what
// the base primitive actually authored. Scene generates flat normals for a
// primitive that has none and tangents for one whose material needs them, and
// those are scene's reconstruction rather than the asset's - so a delta for an
// attribute the file never wrote is dropped rather than added to a value the
// artist did not author.
func TestReadMorphTargetsMasksAgainstAuthoredAttributes(t *testing.T) {
	delta := [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}
	// Every case authors the same three deltas; only what the base primitive
	// declared differs, which is the whole of what the mask answers.
	for _, sample := range []struct {
		name     string
		normals  bool
		tangents bool
		mask     morphMask
		stride   int
	}{
		{name: "no authored normal or tangent", mask: morphPosition, stride: 1},
		{name: "an authored normal", normals: true,
			mask: morphPosition | morphNormal, stride: 2},
		{name: "an authored tangent with no normal behind it", tangents: true,
			mask: morphPosition, stride: 1},
		{name: "everything authored", normals: true, tangents: true,
			mask: morphPosition | morphNormal | morphTangent, stride: 3},
	} {
		t.Run(sample.name, func(t *testing.T) {
			doc := testDoc()
			mesh := morphedMesh(doc, sample.normals, sample.tangents,
				[]gltf.PrimitiveAttributes{deltaTarget(doc, delta, delta, delta)})
			primitive := doc.Meshes[mesh].Primitives[0]
			morph := readMorphTargets(doc, primitive, 3)
			if morph.mask != sample.mask {
				t.Errorf("mask = %b, want %b", morph.mask, sample.mask)
			}
			if morph.stride != sample.stride {
				t.Errorf("stride = %d slots, want %d", morph.stride, sample.stride)
			}
			if got, want := morph.stride*morphRecordSize, sample.stride*16; got != want {
				t.Errorf("record is %d bytes, want 16 * popcount(mask) = %d", got, want)
			}
		})
	}
}

// The shader is handed morphStride and nothing else, so which slots a record
// holds has to be recoverable from the stride alone - which is true only when
// the mask is a prefix of position, normal, tangent. A target that deforms the
// normal and not the position stores an explicit zero position rather than
// closing the gap and being read as a position delta.
func TestReadMorphTargetsWidensAGappedMaskToAPrefix(t *testing.T) {
	doc := testDoc()
	normal := [][3]float32{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}}
	mesh := morphedMesh(doc, true, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, nil, normal, nil),
	})
	morph := readMorphTargets(doc, doc.Meshes[mesh].Primitives[0], 3)
	if want := morphPosition | morphNormal; morph.mask != want {
		t.Fatalf("mask = %b, want the prefix %b", morph.mask, want)
	}
	if morph.deltas[0] != (m.Vec4{}) {
		t.Errorf("vertex 0's position slot = %v, want an explicit zero", morph.deltas[0])
	}
	if got := morph.deltas[1]; got != (m.Vec4{Y: 1}) {
		t.Errorf("vertex 0's normal slot = %v, want the authored delta", got)
	}
}

// The record layout is the whole addressing contract: the shader computes
// morphBase + target*morphTargetStride + vertexIndex*morphStride + slot with no
// table to consult, so a record written anywhere else is read from the wrong
// place with nothing to say so.
//
// The offsets below are spelled out from that formula rather than read back
// through the code that wrote them: a fixture built through the same helper
// agrees with itself whatever the layout is.
func TestReadMorphTargetsLaysRecordsOutTargetMajor(t *testing.T) {
	doc := testDoc()
	mesh := morphedMesh(doc, true, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc,
			[][3]float32{{1, 0, 0}, {2, 0, 0}, {3, 0, 0}},
			[][3]float32{{0, 1, 0}, {0, 2, 0}, {0, 3, 0}}, nil),
		deltaTarget(doc,
			[][3]float32{{0, 0, 4}, {0, 0, 5}, {0, 0, 6}},
			[][3]float32{{7, 0, 0}, {8, 0, 0}, {9, 0, 0}}, nil),
	})
	morph := readMorphTargets(doc, doc.Meshes[mesh].Primitives[0], 3)
	if morph.targets != 2 || morph.stride != 2 {
		t.Fatalf("morph = %d targets of stride %d, want 2 of 2", morph.targets, morph.stride)
	}
	targetStride := 3 * morph.stride
	if got, want := len(morph.deltas), morph.targets*targetStride; got != want {
		t.Fatalf("deltas = %d records, want %d", got, want)
	}
	for _, want := range []struct {
		target, vertex, slot int
		value                m.Vec4
	}{
		{target: 0, vertex: 2, slot: 0, value: m.Vec4{X: 3}},
		{target: 0, vertex: 2, slot: 1, value: m.Vec4{Y: 3}},
		{target: 1, vertex: 0, slot: 0, value: m.Vec4{Z: 4}},
		{target: 1, vertex: 1, slot: 1, value: m.Vec4{X: 8}},
	} {
		at := want.target*targetStride + want.vertex*morph.stride + want.slot
		if got := morph.deltas[at]; got != want.value {
			t.Errorf("target %d vertex %d slot %d = %v, want %v",
				want.target, want.vertex, want.slot, got, want.value)
		}
	}
}

// A primitive with no authored NORMAL is unwelded so its flat normals can
// belong to faces, which renumbers every vertex. A delta is addressed by its
// own vertex's index, so it has to follow - otherwise a morphed face pulls the
// wrong corners, and it does so silently.
func TestConvertPrimitiveRemapsMorphDeltasThroughUnwelding(t *testing.T) {
	doc := testDoc()
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, [][3]float32{{1, 0, 0}, {2, 0, 0}, {3, 0, 0}}, nil, nil),
	})
	primitive := doc.Meshes[mesh].Primitives[0]
	// Reversed indices, so unwelding produces vertices 2, 1, 0 in that order.
	primitive.Indices = gltf.Index(modeler.WriteIndices(doc, []uint16{2, 1, 0}))
	geometry, err := convertPrimitive(doc, primitive, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if geometry.vertices[0].Position != (m.Vec3{Y: 1}) {
		t.Fatalf("unwelding did not run: vertex 0 is %v", geometry.vertices[0].Position)
	}
	want := []float32{3, 2, 1}
	for i, expected := range want {
		if got := geometry.morph.deltas[i].X; got != expected {
			t.Errorf("delta %d = %v, want %v: deltas follow their vertices", i, got, expected)
		}
	}
}

// Bounds expand by the maximum position-delta magnitude summed over the
// targets. It is conservative - it assumes every target at weight 1 at once -
// and it over-draws rather than under-draws, which is the right direction: a
// culled face that should have been on screen is a hole in the picture.
func TestConvertPrimitiveExpandsBoundsByTheMorphReach(t *testing.T) {
	doc := testDoc()
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, [][3]float32{{0, 0, 0}, {2, 0, 0}, {0, 0, 0}}, nil, nil),
		deltaTarget(doc, [][3]float32{{0, 3, 0}, {0, 0, 0}, {0, 0, 0}}, nil, nil),
	})
	geometry, err := convertPrimitive(doc, doc.Meshes[mesh].Primitives[0], false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := geometry.morph.reach; math.Abs(float64(got)-5) > 1e-5 {
		t.Errorf("reach = %v, want the summed maxima 2 + 3", got)
	}
	// The declared box is (0,0,0)..(1,1,0), grown by five in every direction.
	if got, want := geometry.box.Max, (m.Vec3{X: 6, Y: 6, Z: 5}); got != want {
		t.Errorf("box.Max = %v, want %v", got, want)
	}
	if got, want := geometry.box.Min, (m.Vec3{X: -5, Y: -5, Z: -5}); got != want {
		t.Errorf("box.Min = %v, want %v", got, want)
	}
}

// Every morphed primitive's targets concatenate into the one buffer the model
// binds, reached by a base offset. One buffer per primitive would mean a bind
// group per primitive, which collapses group 2's whole reason for existing.
func TestConvertDocumentPacksOneDeltaBufferPerModel(t *testing.T) {
	doc := testDoc()
	delta := [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}
	first := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, delta, nil, nil),
	})
	second := morphedMesh(doc, true, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, delta, delta, nil),
	})
	doc.Nodes = []*gltf.Node{
		{Name: "a", Mesh: gltf.Index(first)},
		{Name: "b", Mesh: gltf.Index(second)},
	}
	sceneOf(doc, 0, 1)
	model := convertTest(t, doc)
	if len(model.primitives) != 2 {
		t.Fatalf("primitives = %d, want two", len(model.primitives))
	}
	// The first block is three vertices of one slot, so the second starts
	// three records in; the second is three vertices of two slots.
	if got := model.primitives[0].morph; got.base != 0 || got.stride != 1 || got.targetStride != 3 {
		t.Errorf("primitive 0 = %+v, want base 0 stride 1 targetStride 3", got)
	}
	if got := model.primitives[1].morph; got.base != 3 || got.stride != 2 || got.targetStride != 6 {
		t.Errorf("primitive 1 = %+v, want base 3 stride 2 targetStride 6", got)
	}
	if got, want := len(model.morphDeltas), 3+6; got != want {
		t.Errorf("the model's delta buffer is %d records, want %d", got, want)
	}
}

// A slot belongs to a node, not a mesh. Two nodes drawing one head morph
// independently - node.weights overrides mesh.weights - so each claims its own
// run of slots and the flattened list carries the same names twice, while the
// deltas behind them stay shared and byte-identical.
func TestConvertDocumentGivesEveryMorphedNodeItsOwnSlots(t *testing.T) {
	doc := testDoc()
	delta := [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, delta, nil, nil),
		deltaTarget(doc, delta, nil, nil),
	})
	doc.Meshes[mesh].Weights = []float64{0.25, 0.5}
	doc.Meshes[mesh].Extras = map[string]any{"targetNames": []any{"smile", "blink"}}
	doc.Nodes = []*gltf.Node{
		{Name: "left", Mesh: gltf.Index(mesh)},
		{Name: "right", Mesh: gltf.Index(mesh), Weights: []float64{1, 0}},
	}
	sceneOf(doc, 0, 1)
	model := convertTest(t, doc)
	if got := model.animation.slotCount; got != 4 {
		t.Fatalf("slots = %d, want two runs of two", got)
	}
	if got := model.animation.targetNames; len(got) != 4 ||
		got[0] != "smile" || got[1] != "blink" || got[2] != "smile" || got[3] != "blink" {
		t.Errorf("target names = %v, want the two runs of the same names", got)
	}
	// node.weights overrides mesh.weights, so the two runs start at different
	// defaults - which is the whole reason the slots are per node.
	want := []float32{0.25, 0.5, 1, 0}
	for slot, expected := range want {
		if got := model.animation.weights[slot]; got != expected {
			t.Errorf("rest weight %d = %v, want %v", slot, got, expected)
		}
	}
	// One block of deltas, shared: the second node's primitive reads the same
	// records the first one does.
	if a, b := model.primitives[0].morph, model.primitives[1].morph; a.base != b.base {
		t.Errorf("the two nodes read blocks at %d and %d, want one shared block", a.base, b.base)
	}
	if a, b := model.primitives[0].morph, model.primitives[1].morph; a.slotBase == b.slotBase {
		t.Errorf("both nodes claim slot base %d, want a run each", a.slotBase)
	}
}

// weightsClip adds an animation that steers one node's morph weights, with
// count scalars per keyframe - which is the shape that makes a weights sampler
// a different decode from a TRS one.
func weightsClip(doc *gltf.Document, name string, node int, times []float32, values []float32) {
	sampler := &gltf.AnimationSampler{
		Input:  modeler.WriteAccessor(doc, gltf.TargetNone, times),
		Output: modeler.WriteAccessor(doc, gltf.TargetNone, values),
	}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name:     name,
		Samplers: []*gltf.AnimationSampler{sampler},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: gltf.TRSWeights},
		}},
	})
}

// A weights-only animation is a real clip with no pose in it: it produces no
// joint, so the model loads with an empty pose buffer and still plays the clip
// by name.
//
// A weights channel steers every slot of the node it targets - the output is
// targetCount scalars per keyframe, not one - so what "a clip leaves alone" is
// is another node's slots, and those stay at the file's defaults.
func TestBakeMorphWeightsSamplesAWeightsOnlyClipOntoTheGrid(t *testing.T) {
	doc := testDoc()
	delta := [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, delta, nil, nil),
		deltaTarget(doc, delta, nil, nil),
	})
	doc.Meshes[mesh].Weights = []float64{0, 0.5}
	doc.Nodes = []*gltf.Node{
		{Name: "face", Mesh: gltf.Index(mesh)},
		{Name: "prop", Mesh: gltf.Index(mesh), Weights: []float64{0.75, 0.25}},
	}
	sceneOf(doc, 0, 1)
	// One second on the first node: slot 0 ramps 0 to 1 while slot 1 holds.
	weightsClip(doc, "smile", 0, []float32{0, 1}, []float32{0, 0.5, 1, 0.5})
	model := convertTest(t, doc)
	animation := &model.animation
	if animation.jointCount != 0 {
		t.Errorf("joints = %d, want none: weights reshape a mesh and leave the node alone",
			animation.jointCount)
	}
	if len(animation.clips) != 1 || animation.clips[0].name != "smile" {
		t.Fatalf("clips = %v, want the one weights-only clip", animation.clips)
	}
	clip := animation.clips[0]
	if clip.frames != 61 {
		t.Fatalf("frames = %d, want the 60 Hz grid's 61", clip.frames)
	}
	// The grid never reaches the GPU, so its width is the model's slot count:
	// two runs of two, one per morphed node.
	if got, want := len(animation.weights), (1+61)*4; got != want {
		t.Errorf("the weight grid is %d floats, want %d", got, want)
	}
	for _, sample := range []struct {
		frame int
		slot0 float32
	}{{frame: 0, slot0: 0}, {frame: 30, slot0: 0.5}, {frame: 60, slot0: 1}} {
		at := clip.weightRow(sample.frame, animation.slotCount)
		if got := animation.weights[at]; math.Abs(float64(got-sample.slot0)) > 1e-5 {
			t.Errorf("frame %d slot 0 = %v, want %v", sample.frame, got, sample.slot0)
		}
		if got := animation.weights[at+1]; got != 0.5 {
			t.Errorf("frame %d slot 1 = %v, want the channel's held 0.5", sample.frame, got)
		}
		// The second node's run is steered by nothing, so it holds the file's
		// defaults in every frame - the morph half of the rule the pose bake
		// follows.
		if got, want := animation.weights[at+2], float32(0.75); got != want {
			t.Errorf("frame %d slot 2 = %v, want the unsteered default %v", sample.frame, got, want)
		}
		if got, want := animation.weights[at+3], float32(0.25); got != want {
			t.Errorf("frame %d slot 3 = %v, want the unsteered default %v", sample.frame, got, want)
		}
	}
	// The rest row is the defaults untouched, which is what a draw with no
	// plays reads.
	for slot, want := range []float32{0, 0.5, 0.75, 0.25} {
		if got := animation.weights[restRow+slot]; got != want {
			t.Errorf("rest slot %d = %v, want the authored default %v", slot, got, want)
		}
	}
}
