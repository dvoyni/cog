package canvas

import "slices"

// shelfAllocator packs rectangles into an area, a page of an atlas, and
// frees each of them: the area is cut into shelves, rows of the page from
// its top to its bottom, each cut into items from its left to its right,
// as etagere packs under WebRender and glyphon. A rectangle goes onto the
// shelf it wastes the fewest rows of, an existing shelf before a new one
// cut from an empty one. Freeing a rectangle merges it with the free items
// beside it, and a shelf left empty with the empty shelves beside it, so
// the area can be taken whole again once all is freed.
type shelfAllocator struct {
	width, height int32
	// shelves cover the area from its top, in order, each below the one
	// before it.
	shelves []shelf
}

// makeShelfAllocator answers an allocator of an area width by height, all
// of it free.
func makeShelfAllocator(width, height int32) shelfAllocator {
	return shelfAllocator{width: width, height: height, shelves: []shelf{{height: height, items: []shelfItem{{width: width}}}}}
}

// allocate answers the top left of a rectangle of width and height it
// took from the area, or false when no free part of it holds one.
func (a *shelfAllocator) allocate(width, height int32) (int32, int32, bool) {
	if width <= 0 || height <= 0 || width > a.width {
		return 0, 0, false
	}
	// The best shelf wastes the fewest rows: an empty one wastes none, as
	// the rectangle cuts a shelf of its own height from it, and an
	// existing one of the same waste is taken first, keeping empty ones
	// whole.
	bestShelf := -1
	bestItem := -1
	var bestWaste int32
	var bestEmpty bool
	for shelfIndex := range a.shelves {
		candidateShelf := &a.shelves[shelfIndex]
		if candidateShelf.height < height {
			continue
		}
		empty := a.isEmptyShelf(shelfIndex)
		waste := candidateShelf.height - height
		if empty {
			waste = 0
		}
		if bestShelf >= 0 && (waste > bestWaste || waste == bestWaste && (empty || !bestEmpty)) {
			continue
		}
		itemIndex := slices.IndexFunc(candidateShelf.items, func(item shelfItem) bool { return !item.allocated && item.width >= width })
		if itemIndex < 0 {
			continue
		}
		bestShelf = shelfIndex
		bestItem = itemIndex
		bestWaste = waste
		bestEmpty = empty
	}
	if bestShelf < 0 {
		return 0, 0, false
	}
	if chosenShelf := &a.shelves[bestShelf]; bestEmpty && chosenShelf.height > height {
		// The rectangle cuts a shelf of its height from the top of the empty
		// one, the rest staying empty below it.
		restShelf := shelf{y: chosenShelf.y + height, height: chosenShelf.height - height, items: []shelfItem{{width: a.width}}}
		chosenShelf.height = height
		a.shelves = slices.Insert(a.shelves, bestShelf+1, restShelf)
	}
	chosenShelf := &a.shelves[bestShelf]
	chosenItem := &chosenShelf.items[bestItem]
	x := chosenItem.x
	if chosenItem.width > width {
		restItem := shelfItem{x: chosenItem.x + width, width: chosenItem.width - width}
		chosenItem.width = width
		chosenShelf.items = slices.Insert(chosenShelf.items, bestItem+1, restItem)
	}
	chosenShelf.items[bestItem].allocated = true
	return x, chosenShelf.y, true
}

// free gives back the rectangle allocate answered at x and y, merging it with
// the free items beside it on its shelf, and its shelf, when that leaves it
// empty, with the empty shelves beside it. A rectangle not allocated there
// panics.
func (a *shelfAllocator) free(x, y int32) {
	shelfIndex := slices.IndexFunc(a.shelves, func(candidateShelf shelf) bool { return candidateShelf.y == y })
	itemIndex := -1
	if shelfIndex >= 0 {
		itemIndex = slices.IndexFunc(a.shelves[shelfIndex].items, func(item shelfItem) bool { return item.x == x && item.allocated })
	}
	if itemIndex < 0 {
		panic("canvas: freeing a rectangle the shelf allocator did not allocate")
	}
	freedShelf := &a.shelves[shelfIndex]
	freedShelf.items[itemIndex].allocated = false
	if nextItem := itemIndex + 1; nextItem < len(freedShelf.items) && !freedShelf.items[nextItem].allocated {
		freedShelf.items[itemIndex].width += freedShelf.items[nextItem].width
		freedShelf.items = slices.Delete(freedShelf.items, nextItem, nextItem+1)
	}
	if previousItem := itemIndex - 1; previousItem >= 0 && !freedShelf.items[previousItem].allocated {
		freedShelf.items[previousItem].width += freedShelf.items[itemIndex].width
		freedShelf.items = slices.Delete(freedShelf.items, itemIndex, itemIndex+1)
	}
	if !a.isEmptyShelf(shelfIndex) {
		return
	}
	if nextShelf := shelfIndex + 1; nextShelf < len(a.shelves) && a.isEmptyShelf(nextShelf) {
		freedShelf.height += a.shelves[nextShelf].height
		a.shelves = slices.Delete(a.shelves, nextShelf, nextShelf+1)
	}
	if previousShelf := shelfIndex - 1; previousShelf >= 0 && a.isEmptyShelf(previousShelf) {
		a.shelves[previousShelf].height += a.shelves[shelfIndex].height
		a.shelves = slices.Delete(a.shelves, shelfIndex, shelfIndex+1)
	}
}

// isEmptyShelf answers whether the shelf at index holds no rectangle, one
// free item as wide as the area.
func (a *shelfAllocator) isEmptyShelf(index int) bool {
	items := a.shelves[index].items
	return len(items) == 1 && !items[0].allocated && items[0].width == a.width
}

// shelf is a row of a shelf allocator's area, from y, height rows tall, its
// items covering it from its left, in order. An empty shelf is one free
// item as wide as the area, which a rectangle of any height up to its own
// cuts a new shelf from.
type shelf struct {
	y, height int32
	items     []shelfItem
}

// shelfItem is part of a shelf, width columns from x, free or holding a
// rectangle as tall as the shelf or less.
type shelfItem struct {
	x, width  int32
	allocated bool
}
