package assets

import "unsafe"

// Blob is a run of bytes treated as static: once a value holding it is built,
// nothing writes the bytes again. It is how an engine type carrying pixels, a
// buffer's contents or a parameter's raw layout says so, and it is what lets
// such a type be an ECS Component - the ECS admits a Blob by type identity
// where it refuses every other slice.
//
// Its identity is the backing array and the length, not the contents: two runs
// spelling the same bytes in two allocations are two Blobs, and re-wrapping the
// same slice is one. That is what makes it comparable, and comparable is what
// lets a Descr holding one be a cache key.
//
// A Blob used as a cache key must therefore be built once and kept. A fresh
// []byte{...} at the call site is a fresh asset every time it runs; a package
// var, a string literal or a const is the shape. Bytes from an arena or a
// per-frame buffer are not - they name an asset that is never found again, and
// the cache fills with entries nothing can ask for twice.
//
// The fields are unexported, so no holder can repoint the bytes under an entry
// keyed on them. Nothing enforces the rest of the contract: Data hands out a
// live slice, and writing through one is an ordinary slice write that no lock
// names and no check sees. Build the bytes, wrap them, and never touch them
// again; bytes that change belong in a new Blob.
//
// There is no Id: nothing in the tree wants to name a Blob, and it stays cheap
// to add - an unexported field and a method, churning no call site.
type Blob struct {
	ptr *byte
	len int
}

// NewBlob wraps b without copying it. It allocates nothing, which is what keeps
// a per-frame descriptor construction free.
//
// An empty b - nil or zero-length - is Blob{}, the canonical empty. That
// short-circuit is not a convenience: unsafe.SliceData on a zero-length slice
// returns an unspecified pointer, so the two rules need each other. The cost is
// that a zero-byte asset cannot be named by its bytes, and a zero-byte asset is
// not a thing.
func NewBlob(b []byte) Blob {
	if len(b) == 0 {
		return Blob{}
	}
	return Blob{ptr: unsafe.SliceData(b), len: len(b)}
}

// NewBlobFromString wraps s without copying it, and is the right way to route
// inline text into an asset. []byte(s) allocates a fresh backing array per
// conversion, so wrapping that instead makes every call its own identity and
// its own cache entry; a string literal, const or package var resolves to the
// same address every evaluation and is therefore one.
//
// A computed string is a fresh allocation like any other, and is one identity
// per computation.
func NewBlobFromString(s string) Blob {
	if len(s) == 0 {
		return Blob{}
	}
	return Blob{ptr: unsafe.StringData(s), len: len(s)}
}

// Data yields the bytes back out without copying them. Blob{} yields nil.
//
// The slice is live, and on a Blob built by NewBlobFromString it points at
// read-only memory, so writing through it faults at the offending write. That
// is the good kind of hazard: the contract is that nothing writes the bytes
// again, and a crash at the write beats silent corruption of a live cache key.
//
// cap equals len, so an append reallocates rather than writing into whatever
// followed the run in an arena.
func (b Blob) Data() []byte {
	if b.ptr == nil {
		return nil
	}
	return unsafe.Slice(b.ptr, b.len)
}

// String yields the bytes back out as text without copying them. Blob{} yields
// the empty string.
func (b Blob) String() string {
	if b.ptr == nil {
		return ""
	}
	return unsafe.String(b.ptr, b.len)
}

// Len reports how many bytes the run holds, so a length test does not
// materialise a slice.
func (b Blob) Len() int { return b.len }
