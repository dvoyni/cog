package mux

import (
	"reflect"
	"unsafe"
)

// pathStep is a path's step through a slice, through a map, or through an
// array at a hole: a literal index of an array compiles into the offset
// instead.
type pathStep struct {
	// kind is the kind of what the step crosses: reflect.Array, at a hole, or
	// reflect.Slice or reflect.Map, at a hole or a literal index.
	kind reflect.Kind
	// offset is where the array, the slice's header or the map lies from the
	// element the step before picks, or from the model's base at the first
	// step.
	offset uintptr
	// index is the index of the element the step picks: a literal one, or
	// holeIndex at a hole, which the next of the holes given fills. A map's
	// index is its key when its keys are integers, and otherwise the index
	// the timeline interned its key at.
	index int64
	// length is the array's length, past which an index misses. A slice's
	// is read from its header.
	length int64
	// elementSize is the size of what the step's index picks among, by which
	// the index is scaled: an array's or a slice's elements, or a map's keys.
	elementSize uintptr
	// mapType is the runtime's descriptor of the map's type, which its map
	// access takes, and mapReflectType the same type as reflection holds it,
	// which a walk iterates the map by.
	mapType        unsafe.Pointer
	mapReflectType reflect.Type
	// mapAccess is the access the step reads the map through, and
	// keyIndicesAccess the one a walk reads keyIndices through: each the
	// one a typed lookup of its map takes.
	mapAccess        mapAccess
	keyIndicesAccess mapAccess
	// keys points at the []K holding each key of the map's type K that the
	// timeline interned, at its index; nil when the map's keys are integers,
	// each its own index.
	keys unsafe.Pointer
	// keyIndices is the map[K]int64 from each key the timeline interned to
	// its index, and keyIndicesType the runtime's descriptor of its type,
	// which a walk looks a key's index up through; nil when the map's keys
	// are integers.
	keyIndices     unsafe.Pointer
	keyIndicesType unsafe.Pointer
	// keyMinimum and keyMaximum are the least and the greatest key of the
	// map's integer key type, as the int64 each is: an index outside them is
	// no key of the type.
	keyMinimum int64
	keyMaximum int64
	// keyOffset is where a key of the map's integer key type lies within the
	// int64 holding its value: at its start on a little-endian machine, and
	// at its end on a big-endian one.
	keyOffset uintptr
}

// findKey answers where the key lies that the hole or literal index at
// index names, and true, or false when no key of the map's type has that
// index: one past its integer type, or one the timeline never interned. A
// key of an integer type is index's own bits, read in place.
func (s *pathStep) findKey(index *int64) (unsafe.Pointer, bool) {
	if s.keys == nil {
		if *index < s.keyMinimum || s.keyMaximum < *index {
			return nil, false
		}
		return unsafe.Add(unsafe.Pointer(index), s.keyOffset), true
	}
	// Every slice's header is laid out alike, so the keys read as a []byte's:
	// its length counts the keys, and its data is where they lie.
	keys := *(*[]byte)(s.keys)
	if uint64(*index) >= uint64(len(keys)) {
		return nil, false
	}
	return unsafe.Add(unsafe.Pointer(unsafe.SliceData(keys)), uintptr(*index)*s.elementSize), true
}

// findKeyHole answers the hole that reaches the map's element under the key
// at key, and true: the key itself, for a map of integer keys, or the index
// the timeline interned it at. It answers false for a key never interned,
// whose element no hole reaches.
func (s *pathStep) findKeyHole(key unsafe.Pointer) (int64, bool) {
	if s.keys != nil {
		// The access is dispatched here, as compiledPath.findProperty
		// dispatches a map step's, so a walk makes no call between this and
		// the runtime for each entry.
		var index unsafe.Pointer
		ok := true
		switch s.keyIndicesAccess {
		case mapAccessFast32:
			index, ok = accessMapFast32(s.keyIndicesType, s.keyIndices, *(*uint32)(key))
		case mapAccessFast64:
			index, ok = accessMapFast64(s.keyIndicesType, s.keyIndices, *(*uint64)(key))
		case mapAccessFastString:
			index, ok = accessMapFastString(s.keyIndicesType, s.keyIndices, *(*string)(key))
		default:
			index = accessMap(s.keyIndicesType, s.keyIndices, key)
			ok = index != nil
		}
		if !ok {
			return 0, false
		}
		return *(*int64)(index), true
	}
	var hole int64
	switch s.elementSize {
	case 1:
		hole = int64(*(*uint8)(key))
	case 2:
		hole = int64(*(*uint16)(key))
	case 4:
		hole = int64(*(*uint32)(key))
	default:
		return *(*int64)(key), true
	}
	// A narrower key's bits read unsigned, so a signed key past its
	// greatest is a negative one, wrapped round its type's range.
	if hole > s.keyMaximum {
		hole -= s.keyMaximum - s.keyMinimum + 1
	}
	return hole, true
}
