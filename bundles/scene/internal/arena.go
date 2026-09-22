package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/slots/gfx"
)

// arena is one frame's staging bytes for one storage binding. Records are
// appended into it and bound back out as ranges, which is how a draw addresses
// its own record without an index anyone has to agree on across the update and
// render threads.
//
// It keeps its backing across frames: a reset truncates, so a steady frame
// allocates nothing after the first.
type arena struct {
	data []byte
}

// reset empties the arena for a new frame without giving up its backing.
func (a *arena) reset() { a.data = a.data[:0] }

// bytes returns the arena's contents, valid until the next reset.
func (a *arena) bytes() []byte { return a.data }

// beginRange pads the arena up to a bindable offset and returns it. A storage
// binding's offset must be a multiple of gfx.StorageAlignment, so anything
// bound as its own range starts here.
func (a *arena) beginRange() int {
	if remainder := len(a.data) % gfx.StorageAlignment; remainder != 0 {
		a.data = append(a.data, make([]byte, gfx.StorageAlignment-remainder)...)
	}
	return len(a.data)
}

// appendRecord appends one bindable record and returns its offset.
func (a *arena) appendRecord[T any](record *T) int {
	offset := a.beginRange()
	a.data = append(a.data, recordBytes(record)...)
	return offset
}

// appendElement appends one element of an array binding, packed tight against
// the element before it: an array's elements are addressed by index inside one
// bound range, not bound separately.
func (a *arena) appendElement[T any](element *T) int {
	offset := len(a.data)
	a.data = append(a.data, recordBytes(element)...)
	return offset
}

// recordBytes reinterprets a record as the bytes uploaded for it. Every GPU
// target cog builds for is little-endian, so the in-memory layout is the wire
// layout — the same reinterpretation canvas's sprite instances use.
func recordBytes[T any](record *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(record)), unsafe.Sizeof(*record))
}
