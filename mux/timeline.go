package mux

import (
	"iter"
)

// timeline is model M's reading side, which Timeline names: a timelineSide
// with the reads only a view makes.
type timeline[M any, D comparable] struct{ timelineSide[M, D] }

// Current answers the current value of the property path names in model, at
// the address holes fill: the blend of the overrides active there in the
// sample the last Step took, and true, or path.Get's answer when none is. It
// reads that sample alone, so an override scheduled or cancelled since shows
// from the next Step.
//
// Holes the path does not take panic, naming the path and their count.
func (t timeline[M, D]) Current[T any](path Path[M, T], model *M, holes ...int64) (T, bool) {
	// Get checks the holes on a path with nothing sampled, so only a path
	// with a sample makes them into an address.
	if state := t.state.findPathState(path); state != nil && len(state.sample) != 0 {
		var filledHoles D
		path.compiled.fillHoles(&filledHoles, holes)
		if value, ok := state.findSampledValue(&filledHoles); ok {
			return value, true
		}
	}
	return path.Get(model, holes...)
}

// All answers a walk over the elements of the last hole of the path in
// model, the holes before it fixed by fixedHoles: for each element, its hole
// and its current value, as Current answers it. A view ranges over it to read
// every element at once, rather than calling Current for each.
//
// Over an array or a slice, the walk goes in index order. Over a map, it goes
// in Go's map order, and yields each key's hole: the key itself, for a map
// keyed by an integer type, or the index the timeline interned it at, so a
// key never interned is skipped, as no hole reaches its element. An element
// with no current value, where an index after the last hole misses, is
// skipped too. Ranging over the walk allocates nothing, to its end or to a
// break.
//
// A path with no hole, or fixed holes other than one fewer than the path's,
// panics, naming the path and their count.
func (t timeline[M, D]) All[T any](path Path[M, T], model *M, fixedHoles ...int64) iter.Seq2[int64, T] {
	return func(yield func(int64, T) bool) { t.state.walk(path, model, fixedHoles, yield) }
}

// Index answers the index the timeline interned key at, which is the hole
// that reaches key's element through a map keyed by K, and true. It answers
// false for a key never interned, whose element no path reaches. It writes
// nothing, so a view looks keys up under the read lock.
//
// A K that keys no map a path of M crosses, or that is an integer type, whose
// keys are their own holes, panics.
func (t timeline[M, D]) Index[K comparable](key K) (int64, bool) {
	index, ok := t.state.findKeyTable[K]("looked up").indices.(map[K]int64)[key]
	return index, ok
}
