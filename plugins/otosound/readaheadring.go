//go:build !js

package otosound

import "sync/atomic"

// unknownStreamLength is what a read-ahead ring answers for its stream's
// length until the read-ahead has reached the clip's end. A looping voice's
// never answers anything else, since a loop has no end.
const unknownStreamLength = ^uint64(0)

// readAheadRing is one streamed voice's read-ahead: frames already decoded
// and already at the rate the mixer produces, waiting for the mixer to copy
// them. The device thread touches it, so it is a fixed buffer and atomic
// counters and nothing else: no lock, no allocation and no signal to wait
// on. The read-ahead fills it and sleeps when it is full; the mixer copies
// out of it, and plays silence for its slot alone when it is empty.
//
// Both counters are absolute frame numbers in the stream rather than indices
// in the buffer, which makes the wrap a mask, since the capacity is a power
// of two, and lets the mixer move on over an underrun without the two sides
// disagreeing about where they are.
type readAheadRing struct {
	// samples is capacity frames, interleaved, channels to a frame. It is
	// made once, when the flush side stages the voice's start, and never
	// reallocated.
	samples []float32
	// capacity is how many frames samples holds, a power of two, and mask is
	// capacity-1, which turns a frame number into an index.
	capacity uint64
	mask     uint64
	channels int
	// produced counts the frames the read-ahead has produced, stored only by
	// it and only once the frames are in samples. passed is the frame the
	// mixer's playhead has passed, stored only by the mixer, which lets the
	// read-ahead overwrite every frame below it.
	produced atomic.Uint64
	passed   atomic.Uint64
	// length is the stream's length in frames once the read-ahead has reached
	// the clip's end, and unknownStreamLength until then: a streamed
	// one-shot ends where the mixer's playhead reaches a length that is
	// known.
	length atomic.Uint64
}

// newReadAheadRing returns an empty ring of at least frames frames of
// channels channels, its capacity rounded up to a power of two.
func newReadAheadRing(frames, channels int) *readAheadRing {
	capacity := uint64(1)
	for capacity < uint64(frames) {
		capacity <<= 1
	}
	ring := &readAheadRing{
		samples:  make([]float32, capacity*uint64(channels)),
		capacity: capacity,
		mask:     capacity - 1,
		channels: channels,
	}
	ring.length.Store(unknownStreamLength)
	return ring
}

// producedFrames is how many frames the read-ahead has produced. A frame
// below it and at or above the one the mixer passed last is in the buffer.
func (r *readAheadRing) producedFrames() uint64 { return r.produced.Load() }

// sampleIndex is where frame's first sample sits in samples: the frame's own
// while it is one the read-ahead produced and the mixer has not passed, and a
// later or earlier frame's otherwise.
func (r *readAheadRing) sampleIndex(frame uint64) int {
	return int((frame & r.mask) * uint64(r.channels))
}

// streamLength is the stream's length in frames, or unknownStreamLength
// while the read-ahead has not reached the clip's end.
func (r *readAheadRing) streamLength() uint64 { return r.length.Load() }

// roomFrames is how many frames the read-ahead may write without overwriting
// one the mixer has not passed. A mixer past every frame produced is an
// underrun that has already happened: its playhead moved on through the
// silence, so everything up to it is room.
func (r *readAheadRing) roomFrames() uint64 {
	produced := r.produced.Load()
	passed := min(r.passed.Load(), produced)
	return r.capacity - (produced - passed)
}

// push writes frames, interleaved, after those already produced, and then
// publishes them: the store of the count is last, so the mixer never reaches
// a frame before its samples are there. It panics when they do not fit in
// roomFrames, since they would overwrite frames the mixer has not passed.
func (r *readAheadRing) push(frames []float32) {
	produced := r.produced.Load()
	frameCount := uint64(len(frames) / r.channels)
	if frameCount > r.roomFrames() {
		panic("otosound: a read-ahead pushed more frames than its ring has room for")
	}
	channels := uint64(r.channels)
	for written := uint64(0); written < frameCount; {
		at := (produced + written) & r.mask
		run := min(frameCount-written, r.capacity-at)
		copy(r.samples[at*channels:], frames[written*channels:(written+run)*channels])
		written += run
	}
	r.produced.Store(produced + frameCount)
}
