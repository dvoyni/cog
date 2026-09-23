package types

import (
	"unsafe"

	"github.com/dvoyni/cog/slots/gfx"
)

// SkinBuffers is the group 2 bindings a draw reads, in either half
// independently. The two flags are also what picks the draw's shader variant, so
// a half left empty is a half the module does not declare.
type SkinBuffers struct {
	Poses  gfx.BufferDescr
	Joints gfx.BufferDescr
	// Morphs is the model's one delta buffer. It is a binding of its own rather
	// than a range of the pose buffer
	// because the two halves are answered separately: a rigged prop has poses
	// and no shapes, and a face has shapes and no poses.
	Morphs gfx.BufferDescr
	// Bound says the pose pair is real and morphed says the delta buffer is. A
	// gfx.BufferDescr holds a byte slice and so is not comparable, and there is
	// no reserved zero descriptor, so "did anyone fill this in" needs a field
	// of its own - two of them, because the halves are answered separately.
	Bound   bool
	Morphed bool
}

// recordSliceBytes reinterprets a slice of records as the bytes uploaded for
// it, the array twin of recordBytes. Every GPU target cog builds for is
// little-endian, so the in-memory layout is the wire layout.
func recordSliceBytes[T any](records []T) []byte {
	if len(records) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(records[0])))
}

// grow returns a slice of exactly n elements, reusing values' backing when it
// is large enough.
func grow[T any](values []T, n int) []T {
	if cap(values) >= n {
		return values[:n]
	}
	return make([]T, n)
}
