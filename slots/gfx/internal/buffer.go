package internal

import (
	"github.com/dvoyni/cog/libs/assets"
)

// bufferSource selects how a BufferDescr is resolved.
type bufferSource int

const (
	BufferSourceBytes bufferSource = iota
	BufferSourceBaked
)

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// baked storage buffer returned by ResourceQueue.BakeBuffer.
type BufferDescr struct {
	source bufferSource
	id     BufferID
	size   int
	// bytes is static: the descriptor never writes it, and a caller who built
	// it from a slice they still hold must not either. That is what lets a
	// Component hold one; see assets.Blob.
	bytes    assets.Blob
	copyData bool
}

// BakedBuffer is the descriptor of a buffer already baked under id.
func BakedBuffer(id BufferID, size int) BufferDescr {
	return BufferDescr{source: BufferSourceBaked, id: id, size: size}
}

// BufferWithBytes describes a buffer from inline bytes. copyData snapshots the
// bytes when recorded if true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped.
func BufferWithBytes(data []byte, copyData bool) BufferDescr {
	return BufferDescr{source: BufferSourceBytes, size: len(data), bytes: assets.NewBlob(data), copyData: copyData}
}

// ID returns the baked buffer identifier, or 0 when the descriptor is not
// baked.
func (b BufferDescr) ID() BufferID { return b.id }

// Size returns the buffer's size in bytes: what was uploaded for an inline
// descriptor, and what the baked buffer holds for a baked one.
func (b BufferDescr) Size() int { return b.size }

// InlineBytes reports how many bytes of inline data the descriptor carries,
// and zero for a baked buffer. The bytes themselves stay inside it, for the
// reason TextureDescr.PixelBytes gives.
func (b BufferDescr) InlineBytes() int { return b.bytes.Len() }

// hasData reports whether the descriptor carries geometry: inline bytes or a
// baked buffer.
func (b BufferDescr) hasData() bool {
	return b.source == BufferSourceBaked || b.bytes.Len() > 0
}
