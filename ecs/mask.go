package ecs

import (
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

// maskBits answers how many component types W holds.
func maskBits[W WorldID]() int {
	var m W
	return int(unsafe.Sizeof(m)) * 8
}

// maskHas reports whether bit id is set in m.
func maskHas[W WorldID](m *W, id ComponentID) bool {
	return atomic.LoadUint64(&maskWords(m)[id>>6])&(1<<(id&63)) != 0
}

// maskSet sets bit id in m, atomically, so writers of different stores can
// update one entity's mask at once.
func maskSet[W WorldID](m *W, id ComponentID) {
	atomic.OrUint64(&maskWords(m)[id>>6], 1<<(id&63))
}

// maskUnset clears bit id in m, atomically.
func maskUnset[W WorldID](m *W, id ComponentID) {
	atomic.AndUint64(&maskWords(m)[id>>6], ^uint64(1<<(id&63)))
}
