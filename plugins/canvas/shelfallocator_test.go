package canvas

import "testing"

// allocatedRect is a rectangle a test allocated, to check rectangles never
// overlap.
type allocatedRect struct{ x, y, width, height int32 }

// overlaps answers whether r and other share a texel.
func (r allocatedRect) overlaps(other allocatedRect) bool {
	return r.x < other.x+other.width && other.x < r.x+r.width && r.y < other.y+other.height && other.y < r.y+r.height
}

// allocateAll allocates each of sizes from allocator, failing t for one that
// does not fit, lies outside the allocator's area or overlaps one before
// it, and answers the rectangles.
func allocateAll(t *testing.T, allocator *shelfAllocator, sizes [][2]int32) []allocatedRect {
	t.Helper()
	var rects []allocatedRect
	for _, size := range sizes {
		x, y, ok := allocator.allocate(size[0], size[1])
		if !ok {
			t.Fatalf("allocating %v after %v did not fit", size, rects)
		}
		rect := allocatedRect{x, y, size[0], size[1]}
		if x < 0 || y < 0 || x+size[0] > allocator.width || y+size[1] > allocator.height {
			t.Fatalf("allocating %v answered %v, outside the allocator's %dx%d", size, rect, allocator.width, allocator.height)
		}
		for _, before := range rects {
			if rect.overlaps(before) {
				t.Fatalf("allocating %v answered %v, overlapping %v", size, rect, before)
			}
		}
		rects = append(rects, rect)
	}
	return rects
}

func TestAShelfAllocatorPacksRectanglesOfOneHeightOnOneShelfAndTallerOnesOnShelvesBelow(t *testing.T) {
	allocator := makeShelfAllocator(32, 32)

	rects := allocateAll(t, &allocator, [][2]int32{{8, 8}, {8, 8}, {16, 12}, {8, 8}})

	want := []allocatedRect{{0, 0, 8, 8}, {8, 0, 8, 8}, {0, 8, 16, 12}, {16, 0, 8, 8}}
	for index := range want {
		if rects[index] != want[index] {
			t.Errorf("the rectangles were allocated at %v, want %v: one shelf of the 8s, the 12 below it", rects, want)
			break
		}
	}
}

func TestAShelfAllocatorRefusesWhatDoesNotFit(t *testing.T) {
	allocator := makeShelfAllocator(16, 16)
	allocateAll(t, &allocator, [][2]int32{{16, 10}})

	if x, y, ok := allocator.allocate(4, 8); ok {
		t.Errorf("allocating 4x8 under a shelf of 10 in 16 rows answered %d, %d", x, y)
	}
	if x, y, ok := allocator.allocate(17, 1); ok {
		t.Errorf("allocating 17x1 in 16 columns answered %d, %d", x, y)
	}
	allocateAll(t, &allocator, [][2]int32{{16, 6}})
}

func TestAShelfAllocatorPutsARectangleOnTheLowestShelfItFits(t *testing.T) {
	allocator := makeShelfAllocator(32, 64)
	allocateAll(t, &allocator, [][2]int32{{4, 20}, {4, 10}, {32, 34}})

	rects := allocateAll(t, &allocator, [][2]int32{{4, 9}})

	if want := (allocatedRect{4, 20, 4, 9}); rects[0] != want {
		t.Errorf("a 4x9 rectangle went to %v, want %v, on the 10 shelf rather than the 20", rects[0], want)
	}
}

func TestAShelfAllocatorFreesARectangleForAnotherAndMergesFreeNeighbours(t *testing.T) {
	allocator := makeShelfAllocator(16, 16)
	rects := allocateAll(t, &allocator, [][2]int32{{4, 8}, {4, 8}, {4, 8}, {4, 8}, {16, 8}})

	allocator.free(rects[1].x, rects[1].y)
	allocator.free(rects[2].x, rects[2].y)

	if got := allocateAll(t, &allocator, [][2]int32{{8, 8}}); got[0] != (allocatedRect{4, 0, 8, 8}) {
		t.Errorf("an 8x8 rectangle went to %v, want the two freed 4s merged at 4, 0", got[0])
	}
}

func TestAShelfAllocatorMergesAnEmptiedShelfWithTheEmptySpaceBesideIt(t *testing.T) {
	allocator := makeShelfAllocator(16, 16)
	rects := allocateAll(t, &allocator, [][2]int32{{16, 4}, {16, 4}, {16, 4}})

	allocator.free(rects[1].x, rects[1].y)
	allocator.free(rects[2].x, rects[2].y)

	if got := allocateAll(t, &allocator, [][2]int32{{16, 12}}); got[0] != (allocatedRect{0, 4, 16, 12}) {
		t.Errorf("a 16x12 rectangle went to %v, want the emptied shelves and the space below merged from row 4", got[0])
	}
	allocator.free(0, 0)
	allocator.free(0, 4)
	if got := allocateAll(t, &allocator, [][2]int32{{16, 16}}); got[0] != (allocatedRect{0, 0, 16, 16}) {
		t.Errorf("once all was freed, a 16x16 rectangle went to %v, want the whole area", got[0])
	}
}

func TestAShelfAllocatorReusesAFreedRectangleForAnotherOfItsSizeAfterManyRounds(t *testing.T) {
	allocator := makeShelfAllocator(64, 64)
	sizes := [][2]int32{{8, 8}, {12, 8}, {20, 16}, {6, 10}, {30, 12}, {7, 7}}
	for round := range 200 {
		rects := allocateAll(t, &allocator, sizes)
		for _, rect := range rects {
			allocator.free(rect.x, rect.y)
		}
		if len(allocator.shelves) != 1 || len(allocator.shelves[0].items) != 1 {
			t.Fatalf("after round %d freed everything, the allocator holds %+v, want one empty shelf", round, allocator.shelves)
		}
	}
}
