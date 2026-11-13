package mux

// overrideHoleOrder is a path's overrides as sort.Interface sorts them, by
// holes. Less compares two overrides in place, where slices.SortStableFunc
// would copy both into its comparison.
type overrideHoleOrder[D comparable, T any] []override[D, T]

func (o *overrideHoleOrder[D, T]) Len() int { return len(*o) }

func (o *overrideHoleOrder[D, T]) Less(firstIndex, secondIndex int) bool {
	return compareHoles(&(*o)[firstIndex].holes, &(*o)[secondIndex].holes) < 0
}

func (o *overrideHoleOrder[D, T]) Swap(firstIndex, secondIndex int) {
	(*o)[firstIndex], (*o)[secondIndex] = (*o)[secondIndex], (*o)[firstIndex]
}
