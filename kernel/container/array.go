package container

// Array is a growing sequence of T, addressed by position from 0. Items live
// in fixed-size chunks that never move, so growing copies nothing, and a
// pointer At returns stays valid until that position is removed or cut off.
//
// It is not synchronized: one goroutine at a time may change it, and readers
// must not overlap a change. Shrinking keeps its chunks, so an Array that has
// reached its working size allocates nothing. The zero Array is empty and
// ready to use.
//
// T should hold no pointers, so the GC never scans the chunks.
type Array[T any] struct {
	chunks []*[chunkSize]T
	length int
}

// Len answers how many items the array holds.
func (a *Array[T]) Len() int {
	return a.length
}

// At returns the item at i, which must be below Len.
func (a *Array[T]) At(i int) *T {
	return &a.chunks[i>>chunkShift][i&chunkMask]
}

// Append adds item at the end and returns its position.
func (a *Array[T]) Append(item T) int {
	i := a.length
	if i == len(a.chunks)<<chunkShift {
		a.chunks = append(a.chunks, new([chunkSize]T))
	}
	a.chunks[i>>chunkShift][i&chunkMask] = item
	a.length++
	return i
}

// Resize makes the array hold length items. Items cut off are gone; items
// added are zero.
func (a *Array[T]) Resize(length int) {
	for len(a.chunks)<<chunkShift < length {
		a.chunks = append(a.chunks, new([chunkSize]T))
	}
	var zero T
	for i := a.length; i < length; i++ {
		a.chunks[i>>chunkShift][i&chunkMask] = zero
	}
	a.length = length
}

// SwapRemove removes the item at i by moving the last item into its place,
// and returns the last item's old position. Only the last item moves.
func (a *Array[T]) SwapRemove(i int) int {
	last := a.length - 1
	*a.At(i) = *a.At(last)
	a.length = last
	return last
}

// Chunk returns the items of chunk c, below Chunks, as a slice that stays
// valid while the array does not shrink into it. Chunks are filled in order
// and the last one's slice is cut at Len. Walking chunk by chunk visits every
// item without resolving each position.
func (a *Array[T]) Chunk(c int) []T {
	start := c << chunkShift
	return a.chunks[c][:min(a.length-start, chunkSize)]
}

// Chunks answers how many chunks hold items.
func (a *Array[T]) Chunks() int {
	return (a.length + chunkMask) >> chunkShift
}
