package internal

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
)

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// baked storage buffer returned by ResourceQueue.BakeBuffer.
type BufferDescr struct {
	source bufferSource
	id     gpu.BufferID
	size   int
	// bytes is static: the descriptor never writes it, and a caller who built
	// it from a slice they still hold must not either. That is what lets a
	// Component hold one; see m.Blob.
	bytes    m.Blob
	copyData bool
}

// ID returns the baked buffer identifier, or 0 when the descriptor is not
// baked.
func (b BufferDescr) ID() gpu.BufferID { return b.id }

// Size returns the buffer's size in bytes: what was uploaded for an inline
// descriptor, and what the baked buffer holds for a baked one.
func (b BufferDescr) Size() int { return b.size }

// InlineBytes reports how many bytes of inline data the descriptor carries,
// and zero for a baked buffer. The bytes themselves stay inside it, for the
// reason TextureDescr.PixelBytes gives.
func (b BufferDescr) InlineBytes() int { return len(b.bytes) }

// bufferSource selects how a BufferDescr is resolved.
type bufferSource int

const (
	BufferSourceBytes bufferSource = iota
	BufferSourceBaked
)

// BakedBuffer is the descriptor of a buffer already baked under id.
func BakedBuffer(id gpu.BufferID, size int) BufferDescr {
	return BufferDescr{source: BufferSourceBaked, id: id, size: size}
}

// BufferWithBytes describes a buffer from inline bytes. copyData snapshots the
// bytes when recorded if true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped.
func BufferWithBytes(data []byte, copyData bool) BufferDescr {
	return BufferDescr{source: BufferSourceBytes, size: len(data), bytes: data, copyData: copyData}
}

// hasData reports whether the descriptor carries geometry: inline bytes or a
// baked buffer.
func (b BufferDescr) hasData() bool {
	return b.source == BufferSourceBaked || len(b.bytes) > 0
}

// TemporaryBuffer uploads one frame-lifetime storage buffer and returns the
// baked descriptor for it, so every draw that binds a range of it shares one
// upload. It is the arena counterpart of TemporaryTarget: BufferWithBytes
// re-bakes wherever it is recorded, which is right for a buffer one draw owns
// and wrong for one the whole frame reads.
//
// copyData snapshots the bytes when true; when false the caller must keep them
// unchanged until the recorded frame is consumed or dropped. Its contents do
// not survive the frame.
func (q *OpQueue) TemporaryBuffer(data []byte, copyData bool) BufferDescr {
	if len(data) == 0 {
		return BufferDescr{}
	}
	return q.temporaryBuffer(gpu.BufferStorage, data, copyData)
}
