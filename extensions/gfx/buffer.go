package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// baked storage buffer returned by ResourceQueue.BakeBuffer.
type BufferDescr = internal.BufferDescr

const (
	BufferSourceBytes = internal.BufferSourceBytes
	BufferSourceBaked = internal.BufferSourceBaked
)

// BufferWithBytes describes a buffer from inline bytes. copyData snapshots the
// bytes when recorded if true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped.
func BufferWithBytes(data []byte, copyData bool) BufferDescr {
	return internal.BufferWithBytes(data, copyData)
}
