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
	return q.temporaryBuffer(BufferStorage, data, copyData)
}
