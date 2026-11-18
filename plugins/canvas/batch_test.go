package canvas

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestBatchesBreakOnlyWhereAnEntrysMaterialDiffersFromTheOneBefore(t *testing.T) {
	firstMaterial := Material{index: 1, generation: 1}
	secondMaterial := Material{index: 2, generation: 1}
	shapes := []Shape{
		{Order: 1, Material: firstMaterial, Tint: m.Color{R: 1, A: 1}},
		{Order: 2, Material: secondMaterial, Rect: m.Rect{Width: 4, Height: 4}},
		{Order: 3, Material: firstMaterial},
		{Order: 0, Material: firstMaterial, Layer: -1},
		{Order: 4, Material: firstMaterial, Transform: m.Translation2x3(1, 2)},
	}
	sorter := makeEntrySorter(nil)

	entries := sorter.sort(shapes)

	got, instances := makeBatches(nil, nil, entries, shapes)

	want := []batch{
		{key: batchKey{material: firstMaterial}, entries: entries[0:2], firstInstance: 0, instanceCount: 2},
		{key: batchKey{material: secondMaterial}, entries: entries[2:3], firstInstance: 2, instanceCount: 1},
		{key: batchKey{material: firstMaterial}, entries: entries[3:5], firstInstance: 3, instanceCount: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the shapes batched as %+v, want %+v: a batch broken where the material changes, and nowhere else", got, want)
	}
	var wantInstances []instanceData
	for _, entry := range entries {
		wantInstances = append(wantInstances, makeShapeInstanceData(shapes[entry.shapeIndex]))
	}
	if !slices.Equal(instances, wantInstances) {
		t.Errorf("the batches' instances are %+v, want each shape's, in sort order, %+v", instances, wantInstances)
	}
}
