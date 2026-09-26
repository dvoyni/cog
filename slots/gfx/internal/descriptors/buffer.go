package descriptors

import (
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// bufferSource selects how a BufferDescr is resolved.
type bufferSource int

const (
	BufferSourceBytes bufferSource = iota
	BufferSourceBaked
)

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// storage buffer returned by ResourceQueue.NewBuffer.
type BufferDescr struct {
	source bufferSource
	id     types.BufferID
	size   int
	// bytes is static: the descriptor never writes it, and a caller who built
	// it from a slice they still hold must not either. That is what lets a
	// Component hold one; see assets.Blob.
	bytes    assets.Blob
	copyData bool
}

// BakedBuffer is the descriptor of a buffer already baked under id.
func BakedBuffer(id types.BufferID, size int) BufferDescr {
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
func (b BufferDescr) ID() types.BufferID { return b.id }

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

// StorageAlignment is the offset alignment a storage binding requires. A record
// a draw binds a range of therefore pads up to a multiple of it - a pad, not a
// cap on what a record may hold.
const StorageAlignment = 256

// IndexWidth is how wide one element of an index buffer is. There are exactly
// two, fixed by the platform rather than chosen: WebGPU has no uint8 index
// format, so geometry that was authored at a byte per index is stored at two.
//
// Nothing about it is a fidelity call - an index is exact or it is broken - so
// gfx neither derives nor validates the choice: it carries whatever width the
// caller declared its bytes to be in, and the only thing it can check is that
// the bytes divide by it.
//
// The zero value is IndexUint32, the width that is legal for any mesh, so a
// descriptor built without naming one is wide rather than wrong.
type IndexWidth uint8

const (
	IndexUint32 IndexWidth = iota
	IndexUint16
)

// Bytes reports how many bytes one index of this width occupies.
func (w IndexWidth) Bytes() int {
	if w == IndexUint16 {
		return 2
	}
	return 4
}

func (w IndexWidth) String() string {
	if w == IndexUint16 {
		return "uint16"
	}
	return "uint32"
}
