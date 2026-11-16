package ecs

import (
	"iter"
	"math/bits"
	"sync/atomic"
	"unsafe"
)

// A mask is a value of a world ID, held per entity: bit ComponentID is set
// when the entity has that component.

// maskWords returns m's words. Their count is fixed per instantiation, so
// every operation on a mask compiles for its size.
func maskWords[W WorldID](m *W) []uint64 {
	return unsafe.Slice((*uint64)(unsafe.Pointer(m)), unsafe.Sizeof(*m)/8)
}

// maskIDs yields the ComponentID of every bit set in m, in ascending order.
// It inlines into its callers, loop body included, and allocates nothing.
func maskIDs[W WorldID](m *W) iter.Seq[ComponentID] {
	return func(yield func(ComponentID) bool) {
		words := maskWords(m)
		for word := range words {
			for set := words[word]; set != 0; set &= set - 1 {
				if !yield(ComponentID(word<<6 + bits.TrailingZeros64(set))) {
					return
				}
			}
		}
	}
}

// maskBits answers how many component types W holds.
func maskBits[W WorldID]() int {
	var m W
	return int(unsafe.Sizeof(m)) * 8
}

// maskWord returns the word of m holding bit id, which must be below
// maskBits, as every registered ComponentID is. It is pointer arithmetic,
// with no bounds check, so the bit operations stay cheap enough to inline
// into their callers.
func maskWord[W WorldID](m *W, id ComponentID) *uint64 {
	return (*uint64)(unsafe.Add(unsafe.Pointer(m), uintptr(id>>6)*8))
}

// maskHas reports whether bit id is set in m.
func maskHas[W WorldID](m *W, id ComponentID) bool {
	return atomic.LoadUint64(maskWord(m, id))&(1<<(id&63)) != 0
}

// maskSet sets bit id in m, atomically, so writers of different stores can
// update one entity's mask at once.
func maskSet[W WorldID](m *W, id ComponentID) {
	atomic.OrUint64(maskWord(m, id), 1<<(id&63))
}

// maskUnset clears bit id in m, atomically.
func maskUnset[W WorldID](m *W, id ComponentID) {
	atomic.AndUint64(maskWord(m, id), ^uint64(1<<(id&63)))
}
