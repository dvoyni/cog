package types

import (
	"encoding/json"

	"github.com/dvoyni/cog/libs/assets"
)

// BufferDescr describes a GPU buffer from inline bytes (BufferDescrWithBlob)
// or a storage buffer returned by ResourceQueue.NewBuffer
// (BufferDescrWithId).
//
// The cases are disjoint and are told apart by which field carries the answer:
// an ID for a baked buffer, Bytes for inline data, and neither for no buffer at
// all - the index buffer of a non-indexed mesh. There is no source enum,
// because it restated exactly that; IDs are minted from 1, so zero names none.
type BufferDescr struct {
	// Bytes is static: the descriptor never writes it, and a caller who built
	// it from a slice they still hold must not either. That is what lets a
	// Component hold one; see assets.Blob.
	Bytes assets.Blob
	// Size is the buffer's size in bytes: what was uploaded for an inline
	// descriptor, and what the baked buffer holds for a baked one.
	Size int
	// ID is the baked buffer's identifier, and zero when the descriptor is not
	// baked.
	ID BufferID
	// CopyData snapshots Bytes when the descriptor is recorded. When false,
	// the caller must keep them unchanged until the recorded frame is consumed
	// or dropped.
	CopyData bool
}

// BufferDescrWithId is the descriptor of a buffer already baked under id.
func BufferDescrWithId(id BufferID, size int) BufferDescr {
	return BufferDescr{ID: id, Size: size}
}

// BufferDescrWithBlob describes a buffer from inline bytes. copyData snapshots
// the bytes when recorded if true; when false, the caller must keep them
// unchanged until the recorded frame is consumed or dropped.
func BufferDescrWithBlob(data assets.Blob, copyData bool) BufferDescr {
	return BufferDescr{Size: data.Len(), Bytes: data, CopyData: copyData}
}

// MarshalJSON reports where the buffer comes from and its size, and the inline
// bytes it carries as a count, for the reason TextureDescr gives.
func (b BufferDescr) MarshalJSON() ([]byte, error) {
	source := "none"
	switch {
	case b.ID != 0:
		source = "baked"
	case b.Bytes.Len() != 0:
		source = "bytes"
	}
	return json.Marshal(struct {
		Source string   `json:"source"`
		ID     BufferID `json:"id,omitempty"`
		Size   int      `json:"size,omitempty"`
		Bytes  int      `json:"bytes,omitempty"`
	}{source, b.ID, b.Size, b.Bytes.Len()})
}
