package canvas

import "sync"

// internTable interns keys, such as an image's path and options or a mesh
// file's path, into indices: each distinct key once, never released, since
// a game's paths are a bounded set. A key's index is its place in the
// table, plus 1, so the zero index names none.
type internTable[K comparable] struct {
	// mutex guards indices, each key's index, and keys, each index's key at
	// the index minus 1.
	mutex   sync.RWMutex
	indices map[K]uint32
	keys    []K
}

// internKey answers key's index, interning key the first time it is asked.
func (t *internTable[K]) internKey(key K) uint32 {
	t.mutex.RLock()
	index, interned := t.indices[key]
	t.mutex.RUnlock()
	if interned {
		return index
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if index, interned := t.indices[key]; interned {
		return index
	}
	if t.indices == nil {
		t.indices = map[K]uint32{}
	}
	t.keys = append(t.keys, key)
	index = uint32(len(t.keys))
	t.indices[key] = index
	return index
}

// findPaths answers the path of each key interned that Unload(unloaded)
// unloads, pathOf answering a key's path, and false for a key of none, a
// path interned in several keys once for each.
func (t *internTable[K]) findPaths(unloaded string, pathOf func(K) (string, bool)) []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	var paths []string
	for _, key := range t.keys {
		if path, hasPath := pathOf(key); hasPath && isUnder(path, unloaded) {
			paths = append(paths, path)
		}
	}
	return paths
}

// findKeyAt answers the key interned at index, which is not zero.
func (t *internTable[K]) findKeyAt(index uint32) K {
	if index == 0 {
		panic("canvas: finding the key of index 0, which names none")
	}
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.keys[index-1]
}
