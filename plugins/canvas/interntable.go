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

// findKeyAt answers the key interned at index, which is not zero.
func (t *internTable[K]) findKeyAt(index uint32) K {
	if index == 0 {
		panic("canvas: finding the key of index 0, which names none")
	}
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.keys[index-1]
}

// imageNames is the process's table of the images ImageFromFile interned, which
// every kernel's canvas reads its images' paths and options from. An
// Image's index is its key's place in it, plus 1, so the zero Image names
// none.
var imageNames imageTable

// imageTable interns image keys into Images.
type imageTable struct {
	internTable[imageKey]
}

// findKey answers the key image was interned from, image not the zero one.
func (t *imageTable) findKey(image Image) imageKey {
	return t.findKeyAt(image.index)
}

// meshFileNames is the process's table of the mesh files MeshFileFromFile
// interned, by path, which every kernel's canvas reads their paths from. A
// MeshFile's index is its path's place in it, plus 1, so the zero MeshFile
// names none.
var meshFileNames internTable[string]
