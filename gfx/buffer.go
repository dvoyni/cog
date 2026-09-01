package gfx

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// baked storage buffer returned by ResourceQueue.BakeBuffer.
type BufferDescr struct {
	source   bufferSource
	id       BufferID
	size     int
	bytes    []byte
	copyData bool
}

// bufferSource selects how a BufferDescr is resolved.
type bufferSource int

const (
	BufferSourceBytes bufferSource = iota
	BufferSourceBaked
)

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
