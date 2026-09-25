package types

import "strconv"

// BufferKind tags a buffer's role, which selects its GPU usage flags.
type BufferKind uint8

const (
	BufferVertex BufferKind = iota
	BufferIndex
	BufferUniform
	BufferStorage
)

// Name spells the buffer kind for a debug document.
func (kind BufferKind) String() string {
	switch kind {
	case BufferVertex:
		return "vertex"
	case BufferIndex:
		return "index"
	case BufferUniform:
		return "uniform"
	case BufferStorage:
		return "storage"
	}
	return "unknown(" + strconv.Itoa(int(kind)) + ")"
}

// BufferDesc describes a GPU buffer to create.
type BufferDesc struct {
	Kind  BufferKind
	Size  int
	Label string
}
