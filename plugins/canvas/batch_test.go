package canvas

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// shapeImages are the drawn images of a frame of shapes alone: the white
// texel's, at a region of its own.
var shapeImages = [][]drawnImage{{{region: m.Rect{X: 0.25, Y: 0.5, Width: 0.125, Height: 0.125}, page: 1, size: m.Vec2{X: 1, Y: 1}}}}

func TestBatchesBreakOnlyWhereAnEntrysMaterialDiffersFromTheOneBefore(t *testing.T) {
	firstMaterial := Material{index: 1, generation: 1}
	secondMaterial := Material{index: 2, generation: 1}
	shapes := []Shape{
		{Order: 1, Material: firstMaterial, Tint: m.Color{R: 1, A: 1}},
		{Order: 2, Material: secondMaterial, Kind: Stroke, Rect: m.Rect{Width: 4, Height: 4}},
		{Order: 3, Material: firstMaterial},
		{Order: 0, Material: firstMaterial, Layer: -1},
		{Order: 4, Material: firstMaterial, Transform: m.Translation2x3(1, 2)},
	}
	state := newShapeFrame(shapes, makeEntrySorter(nil))
	entries := state.sorter.entries

	var instances listInstanceData
	got := appendBatches(nil, &instances, entries, state, findShapeBatchKeys(state), shapeImages, nil)

	want := []batch{
		{key: batchKey{material: firstMaterial}, entries: entries[0:2], firstInstance: 0, instanceCount: 2},
		{key: batchKey{material: secondMaterial}, entries: entries[2:3], firstInstance: 2, instanceCount: 4},
		{key: batchKey{material: firstMaterial}, entries: entries[3:5], firstInstance: 6, instanceCount: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the shapes batched as %+v, want %+v: a batch broken where the material changes, and nowhere else, a stroke drawing four instances", got, want)
	}
	var wantInstances []instanceData
	for _, entry := range entries {
		wantInstances = appendShapeInstances(wantInstances, &shapes[entry.entryIndex], &shapeImages[0][0])
	}
	if !slices.Equal(instances.fixed, wantInstances) {
		t.Errorf("the batches' instances are %+v, want each shape's, in sort order, %+v", instances.fixed, wantInstances)
	}
}

func TestBatchesAppendedAfterAnotherCamerasBeginANewBatchEvenOfTheSameMaterial(t *testing.T) {
	shapes := []Shape{{}, {Kind: Stroke}}
	state := newShapeFrame(shapes, makeEntrySorter(nil))
	entries := state.sorter.entries
	var instances listInstanceData
	firstBatches := appendBatches(nil, &instances, entries[:1], state, findShapeBatchKeys(state), shapeImages, nil)

	got := appendBatches(firstBatches, &instances, entries[1:], state, findShapeBatchKeys(state), shapeImages, nil)

	want := []batch{
		{entries: entries[0:1], firstInstance: 0, instanceCount: 1},
		{entries: entries[1:2], firstInstance: 1, instanceCount: 4},
	}
	if !reflect.DeepEqual(got, want) || len(instances.fixed) != 5 {
		t.Errorf("a camera's fill, then another's stroke, batched as %+v of %d instances, want a batch each, %+v, of 5", got, len(instances.fixed), want)
	}
}

func TestBatchesBreakWhereAnEntrysClipDiffersFromTheOneBefore(t *testing.T) {
	clip := m.Rect{Width: 10, Height: 10}
	shapes := []Shape{{Order: 1}, {Order: 2, Clip: clip}, {Order: 3, Clip: clip}, {Order: 4}}
	state := newShapeFrame(shapes, makeEntrySorter(nil))
	entries := state.sorter.entries

	got := appendBatches(nil, &listInstanceData{}, entries, state, findShapeBatchKeys(state), shapeImages, nil)

	want := []batch{
		{entries: entries[0:1], firstInstance: 0, instanceCount: 1},
		{key: batchKey{clip: clip}, entries: entries[1:3], firstInstance: 1, instanceCount: 2},
		{entries: entries[3:4], firstInstance: 3, instanceCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("an unclipped fill, two clipped alike and another unclipped batched as %+v, want %+v", got, want)
	}
}

func TestInstancesOfAMaterialTakingAnArgumentGoIntoItsStridesStreamEachFollowedByItsArgument(t *testing.T) {
	// A material taking a float32 draws a fill given 2, a stroke given 3,
	// four rows of it, and a fill given none, whose argument's bytes are
	// zeroed over a stream reused from a frame before; a fill of the
	// default material between them draws a fixed instance.
	material := Material{index: 1, generation: 1}
	shapes := []Shape{
		{Material: material, Tint: m.Color{R: 1, A: 1}},
		{Material: material, Kind: Stroke, Rect: m.Rect{Width: 4, Height: 4}},
		{Material: material},
		{Order: 1},
	}
	state := newShapeFrame(shapes, makeEntrySorter(nil))
	argumentType := reflect.TypeFor[float32]()
	state.arguments = castToBytes([]float32{2, 3})
	for index := range 2 {
		state.entries[index].argument = recordedArgument{goType: argumentType, offset: uint32(4 * index)}
	}
	keys := findShapeBatchKeys(state)
	for index := range 3 {
		keys[index].stride = 160
	}
	instances := listInstanceData{streams: []instanceStream{{stride: 160, data: slices.Repeat([]byte{0xff}, 6*160)}}}
	instances.reset()

	got := appendBatches(nil, &instances, state.sorter.entries, state, keys, shapeImages, nil)

	entries := state.sorter.entries
	want := []batch{
		{key: keys[0], entries: entries[0:3], firstInstance: 0, instanceCount: 6},
		{key: keys[3], entries: entries[3:4], firstInstance: 0, instanceCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the shapes batched as %+v, want %+v: the material's six rows from its stream's first, the default material's one fixed instance from the first", got, want)
	}
	var wantRows []byte
	for index, argument := range []float32{2, 3, 0} {
		for _, instance := range appendShapeInstances(nil, &shapes[index], &shapeImages[0][0]) {
			row := make([]byte, 160)
			copy(row, castToBytes([]instanceData{instance}))
			copy(row[instanceDataSize:], castToBytes([]float32{argument}))
			wantRows = append(wantRows, row...)
		}
	}
	stream := instances.findStream(160)
	if len(instances.streams) != 1 || !slices.Equal(stream.data, wantRows) {
		t.Errorf("the material's stream holds %d bytes, want %d, each row its instance, its argument and zeros to 160 bytes, in %d streams, want one", len(stream.data), len(wantRows), len(instances.streams))
	}
	if wantFixed := appendShapeInstances(nil, &shapes[3], &shapeImages[0][0]); !slices.Equal(instances.fixed, wantFixed) {
		t.Errorf("the fixed instances are %+v, want the default material's fill's alone, %+v", instances.fixed, wantFixed)
	}
}

func TestMaterialsWhoseArgumentsRoundUpToOneStrideShareItsStream(t *testing.T) {
	for _, test := range []struct {
		argumentType reflect.Type
		want         uint32
	}{
		{nil, 0},
		{reflect.TypeFor[float32](), 160},
		{reflect.TypeFor[[4]float32](), 160},
		{reflect.TypeFor[[17]byte](), 176},
		{reflect.TypeFor[[8]float32](), 176},
	} {
		if got := findInstanceStride(test.argumentType); got != test.want {
			t.Errorf("an argument of %v is laid %d apart, want %d", test.argumentType, got, test.want)
		}
	}
}
