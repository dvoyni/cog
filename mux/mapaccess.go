package mux

import (
	"reflect"
	"unsafe"
)

// mapAccess is which of the runtime's map accesses a map step reads its map
// through: the one the compiler picks for a typed lookup of the same map.
type mapAccess uint8

const (
	// mapAccessGeneric is reflect.mapaccess, which reads any map, hashing
	// its key through the key type's hash function and comparing keys
	// through its equality.
	mapAccessGeneric mapAccess = iota
	// mapAccessFast32 and mapAccessFast64 are runtime.mapaccess2_fast32 and
	// runtime.mapaccess2_fast64, which read a map whose keys compare as 4 or
	// 8 bytes of plain memory, comparing them inline and skipping the hash
	// in a small map.
	mapAccessFast32
	mapAccessFast64
	// mapAccessFastString is runtime.mapaccess2_faststr, which reads a map
	// whose keys compare as strings.
	mapAccessFastString
)

// maximumFastElementSize is the size of the largest element a map keeps in its
// slots, abi.MapMaxElemBytes: a larger one is kept through a pointer, which
// none of the fast accesses follows.
const maximumFastElementSize = 128

// makeMapAccess answers the access the compiler picks for a typed lookup of
// mapType: a fast one for keys that compare as 4 or 8 bytes of plain memory,
// or as a string, when the map keeps its elements in its slots, and the
// generic one for every other map.
func makeMapAccess(mapType reflect.Type) mapAccess {
	if mapType.Elem().Size() > maximumFastElementSize {
		return mapAccessGeneric
	}
	keyType := mapType.Key()
	switch {
	case isComparedAsString(keyType):
		return mapAccessFastString
	case keyType.Size() == 4 && isComparedAsMemory(keyType):
		return mapAccessFast32
	case keyType.Size() == 8 && isComparedAsMemory(keyType):
		return mapAccessFast64
	}
	return mapAccessGeneric
}

// isComparedAsMemory answers whether a value of keyType compares as its
// bytes, as the compiler's AMEM: a boolean, an integer, a pointer or a
// channel, or an array or a struct of those with no blank field and no
// padding. A float compares otherwise, as +0 equals -0.
func isComparedAsMemory(keyType reflect.Type) bool {
	switch keyType.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Pointer, reflect.UnsafePointer, reflect.Chan:
		return true
	case reflect.Array:
		return keyType.Len() == 0 || isComparedAsMemory(keyType.Elem())
	case reflect.Struct:
		// Fields never overlap, so a struct is padded, between its fields or
		// after its last, where they sum to less than its size.
		var fieldSizeSum uintptr
		for fieldIndex := range keyType.NumField() {
			field := keyType.Field(fieldIndex)
			if field.Name == "_" || !isComparedAsMemory(field.Type) {
				return false
			}
			fieldSizeSum += field.Type.Size()
		}
		return fieldSizeSum == keyType.Size()
	}
	return false
}

// isComparedAsString answers whether a value of keyType compares as a
// string, as the compiler's ASTRING: a string, or a struct of one field or
// an array of one element that does.
func isComparedAsString(keyType reflect.Type) bool {
	switch keyType.Kind() {
	case reflect.String:
		return true
	case reflect.Array:
		return keyType.Len() == 1 && isComparedAsString(keyType.Elem())
	case reflect.Struct:
		return keyType.NumField() == 1 && keyType.Field(0).Name != "_" && isComparedAsString(keyType.Field(0).Type)
	}
	return false
}

// accessMap answers where the element lies that the map at mapPointer, of
// the type whose runtime descriptor is mapType, holds under the key at key,
// or nil when it holds none, a nil map included. It is reflect's map access,
// which the runtime keeps for packages outside it, its signature pledged
// unchanged (go.dev/issue/67401).
//
//go:linkname accessMap reflect.mapaccess
//go:noescape
func accessMap(mapType, mapPointer, key unsafe.Pointer) unsafe.Pointer

// accessMapFast32 answers where the element lies that the map at
// mapPointer, of the type whose runtime descriptor is mapType, holds under
// key, and true, or a pointer to a zero value and false when it holds none,
// a nil map included. It is the runtime's access for a typed lookup of a map
// whose keys compare as 4 bytes of plain memory, which the runtime keeps for
// packages outside it, its signature pledged unchanged (go.dev/issue/67401).
//
//go:linkname accessMapFast32 runtime.mapaccess2_fast32
//go:noescape
func accessMapFast32(mapType, mapPointer unsafe.Pointer, key uint32) (unsafe.Pointer, bool)

// accessMapFast64 is accessMapFast32 for a map whose keys compare as 8 bytes
// of plain memory, runtime.mapaccess2_fast64, under the same pledge.
//
//go:linkname accessMapFast64 runtime.mapaccess2_fast64
//go:noescape
func accessMapFast64(mapType, mapPointer unsafe.Pointer, key uint64) (unsafe.Pointer, bool)

// accessMapFastString is accessMapFast32 for a map whose keys compare as a
// string, runtime.mapaccess2_faststr, under the same pledge.
//
//go:linkname accessMapFastString runtime.mapaccess2_faststr
//go:noescape
func accessMapFastString(mapType, mapPointer unsafe.Pointer, key string) (unsafe.Pointer, bool)
