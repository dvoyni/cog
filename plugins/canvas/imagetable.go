package canvas

import "sync"

// imageNames is the process's table of the images ImageFromFile interned, which
// every kernel's canvas reads its images' paths and options from. An
// Image's index is its key's place in it, plus 1, so the zero Image names
// none.
var imageNames = imageTable{indices: map[imageKey]uint32{}}

// imageTable interns image keys into Images: each distinct key once, never
// released, since a game's paths are a bounded set.
type imageTable struct {
	// mutex guards indices, each key's Image index, and keys, each Image's
	// key at its index minus 1.
	mutex   sync.RWMutex
	indices map[imageKey]uint32
	keys    []imageKey
}

// intern answers key's Image, interning key the first time it is asked.
func (t *imageTable) intern(key imageKey) Image {
	t.mutex.RLock()
	index, interned := t.indices[key]
	t.mutex.RUnlock()
	if interned {
		return Image{index: index}
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if index, interned := t.indices[key]; interned {
		return Image{index: index}
	}
	t.keys = append(t.keys, key)
	index = uint32(len(t.keys))
	t.indices[key] = index
	return Image{index: index}
}

// findKey answers the key image was interned from, image not the zero one.
func (t *imageTable) findKey(image Image) imageKey {
	if image == (Image{}) {
		panic("canvas: finding the key of the zero Image, which ImageFromFile never interns")
	}
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.keys[image.index-1]
}
