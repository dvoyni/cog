package mux

import "reflect"

// keyTable is a timeline's table of the keys of one type K that a map a
// path crosses is keyed by, a K that is no integer: each key the timeline
// interned, by its index, and the other way round. It never shrinks.
type keyTable struct {
	// keyType is K, by which MakePath finds the table.
	keyType reflect.Type
	// indices is the map[K]int64 from each key interned to its index.
	indices any
	// keys is the *[]K holding each key interned at its index, which a map
	// step reads to turn a hole back into its key.
	keys any
}

// newKeyTable answers an empty table of keys of keyType, made by
// reflection, as MakePath makes one for each map a path crosses whose keys
// are interned.
func newKeyTable(keyType reflect.Type) *keyTable {
	return &keyTable{
		keyType: keyType,
		indices: reflect.MakeMap(reflect.MapOf(keyType, reflect.TypeFor[int64]())).Interface(),
		keys:    reflect.New(reflect.SliceOf(keyType)).Interface(),
	}
}

// intern answers key's index: the one it was given first, or, for a key new
// to t, the next.
func (t *keyTable) intern[K comparable](key K) int64 {
	indices := t.indices.(map[K]int64)
	if index, ok := indices[key]; ok {
		return index
	}
	keys := t.keys.(*[]K)
	index := int64(len(*keys))
	*keys = append(*keys, key)
	indices[key] = index
	return index
}
