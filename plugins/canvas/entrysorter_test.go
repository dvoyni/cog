package canvas

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/render"
)

func TestEntriesSortByLayerThenOrderKeepingTheirMergedOrderBetweenEqualKeys(t *testing.T) {
	shapes := []Shape{
		{Layer: 1, Order: 0},
		{Layer: -1, Order: 5},
		{Layer: 0, Order: math.MaxInt32},
		{Layer: 0, Order: math.MinInt32},
		{Layer: 1, Order: 0},
		{Layer: math.MinInt32, Order: 0},
		{Layer: 0, Order: -1},
		{Layer: math.MaxInt32, Order: math.MinInt32},
		{Layer: 1, Order: 0},
		{Layer: -1, Order: -5},
	}
	sorter := makeEntrySorter(nil)

	got := findEntryIndices(newShapeFrame(shapes, sorter).sorter.entries)

	want := []uint32{5, 9, 1, 3, 6, 2, 0, 4, 8, 7}
	if !slices.Equal(got, want) {
		t.Errorf("the shapes sorted as %v, want %v: by layer, then order, then as merged", got, want)
	}
}

func TestEntriesSortAsAStableSortByLayerThenOrderDoes(t *testing.T) {
	generator := rand.New(rand.NewPCG(3, 4))
	sorter := makeEntrySorter(nil)
	for _, layerCount := range []int32{1, 3, 300, math.MaxInt32} {
		shapes := make([]Shape, 2000)
		for index := range shapes {
			shapes[index] = Shape{Layer: generator.Int32N(layerCount) - layerCount/2, Order: int32(generator.Uint32() >> generator.UintN(32))}
		}
		want := make([]uint32, len(shapes))
		for index := range want {
			want[index] = uint32(index)
		}
		slices.SortStableFunc(want, func(a, b uint32) int {
			return cmp.Or(cmp.Compare(shapes[a].Layer, shapes[b].Layer), cmp.Compare(shapes[a].Order, shapes[b].Order))
		})

		got := findEntryIndices(newShapeFrame(shapes, sorter).sorter.entries)

		if !slices.Equal(got, want) {
			t.Errorf("2000 shapes of %d layers sorted otherwise than a stable sort by layer, then order", layerCount)
		}
	}
}

func TestEntriesOfEqualKeysInARegroupedLayerRegroupByBatchInTheOrderTheirBatchesFirstAppear(t *testing.T) {
	firstMaterial := Material{index: 1, generation: 1}
	secondMaterial := Material{index: 2, generation: 1}
	thirdMaterial := Material{index: 3, generation: 1}
	shapes := []Shape{
		{Layer: 3, Material: firstMaterial},
		{Layer: 3, Material: secondMaterial},
		{Layer: 2, Material: firstMaterial},
		{Layer: 3, Material: firstMaterial},
		{Layer: 3, Order: 1, Material: secondMaterial},
		{Layer: 3, Material: thirdMaterial},
		{Layer: 2, Material: secondMaterial},
		{Layer: 3, Material: secondMaterial},
		{Layer: 3, Order: 1, Material: firstMaterial},
		{Layer: 2, Material: firstMaterial},
		{Layer: 3, Order: 1, Material: secondMaterial},
	}
	sorter := makeEntrySorter([]int32{5, 3, 5})

	got := findEntryIndices(newShapeFrame(shapes, sorter).sorter.entries)

	want := []uint32{
		2, 6, 9, // layer 2 is not regrouped
		0, 3, 1, 7, 5, // layer 3's order 0: its first material's, then the second's, then the third's
		4, 10, 8, // its order 1, regrouped apart from order 0, the second material's first
	}
	if !slices.Equal(got, want) {
		t.Errorf("the shapes sorted as %v, want %v", got, want)
	}
}

// findEntryIndices answers the place of each of entries among the entries
// sorted.
func findEntryIndices(entries []sortedEntry) []uint32 {
	indices := make([]uint32, 0, len(entries))
	for _, entry := range entries {
		indices = append(indices, entry.entryIndex)
	}
	return indices
}

// newShapeFrame answers the draw state of a frame of shapes alone, merged
// in their order, each entry's place its shape's, sorted by sorter and
// regrouped by their batch keys, whose entries are the frame's in sort
// order.
func newShapeFrame(shapes []Shape, sorter entrySorter) *drawState {
	state := &drawState{shapes: shapes, sorter: sorter}
	for index := range shapes {
		state.entries = append(state.entries, recordedEntry{kind: EntryKindShape, index: uint32(index)})
	}
	state.findSortKeys()
	state.sorter.sortByKey()
	state.sorter.regroup(findShapeBatchKeys(state))
	return state
}

// findShapeBatchKeys answers the batch keys of the shapes of state, a frame
// of shapes alone, each at its entry's index, over the zero atlas.
func findShapeBatchKeys(state *drawState) []batchKey {
	var keys []batchKey
	for index := range state.shapes {
		keys = append(keys, state.shapes[index].batchKey(render.TextureView{}))
	}
	return keys
}

// sortedEntryCount is how many entries BenchmarkSortByKey sorts.
const sortedEntryCount = 100000

// BenchmarkSortByKey benchmarks the radix sort of sortedEntryCount entries,
// reporting the time per entry, as their keys spread over more of their
// bytes, each a pass: entries of one layer and order, sorted in no pass; of
// 4 layers, one pass; of 4 layers y-sorted, five; and of layers and orders
// spread over all 32 bits each, eight.
func BenchmarkSortByKey(b *testing.B) {
	random := rand.New(rand.NewPCG(1, 2))
	for _, spread := range []struct {
		name string
		key  func(index int) sortKey
	}{
		{"passes=0", func(int) sortKey { return makeSortKey(0, 0) }},
		{"passes=1", func(index int) sortKey { return makeSortKey(int32(index%4), 0) }},
		{"passes=5", func(index int) sortKey { return makeSortKey(int32(index%4), OrderOf(random.Float32()*1000)) }},
		{"passes=8", func(int) sortKey { return makeSortKey(random.Int32()-math.MaxInt32/2, random.Int32()-math.MaxInt32/2) }},
	} {
		b.Run(spread.name, func(b *testing.B) {
			var merged []sortedEntry
			for index := range sortedEntryCount {
				merged = append(merged, sortedEntry{key: spread.key(index), entryIndex: uint32(index)})
			}
			sorter := entrySorter{entries: make([]sortedEntry, sortedEntryCount)}
			for b.Loop() {
				// Each frame's entries come to the sort in the order merged.
				sorter.entries = sorter.entries[:sortedEntryCount]
				copy(sorter.entries, merged)
				sorter.sortByKey()
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*sortedEntryCount), "ns/entry")
		})
	}
}
