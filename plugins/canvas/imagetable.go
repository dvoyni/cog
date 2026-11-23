package canvas

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
