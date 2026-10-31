//go:build !js

package internal

import "sync/atomic"

// unknownLength is what a read-ahead ring reports for its stream's length until
// the read-ahead has reached the end of the Clip. A looping Voice never sets
// anything else, because it has no end.
const unknownLength = ^uint64(0)

// pcmRing is one streamed Voice's read-ahead: frames already decoded, already
// converted to the device rate, waiting for the Mixer to copy them.
//
// It is the streamed tier's half of the same bargain the batch ring makes. The
// device thread may touch it, so it is two monotonic counters and a fixed
// buffer and nothing else - no lock, no allocation, no signal to wait on. The
// read-ahead goroutine fills it and sleeps when it is full; the Mixer copies
// out of it and plays silence for that slot when it is empty, which is the one
// thing an underrun may never do to the rest of the mix.
//
// Both counters are absolute frame numbers in the stream rather than indices in
// the buffer, which is what makes the wrap arithmetic a mask and what lets the
// Mixer skip forward over an underrun without the two sides disagreeing about
// where in the Clip they are.
type pcmRing struct {
	// samples is frames x channels, interleaved, at the device rate. It is made
	// once, when the Voice starts, and is never reallocated.
	samples []float32
	// frames is the capacity and mask is frames-1: the capacity is a power of
	// two so that an absolute frame number becomes an index with an AND rather
	// than a division on the device thread.
	frames, mask uint64
	channels     int

	// write is the number of frames the read-ahead has produced, stored only by
	// it and only once the frames themselves are in the buffer. read is how far
	// the Mixer's playhead has passed, stored only by the Mixer. Between them
	// they are the whole of the synchronisation.
	write, read atomic.Uint64
	// total is the stream's length in frames once the read-ahead has reached
	// the end of the Clip, and unknownLength until then. It is how a streamed
	// one-shot ends: the Mixer's playhead reaching a length that is known.
	total atomic.Uint64
}

// newPCMRing makes a ring holding at least the read-ahead a Voice needs. It is
// the per-Voice allocation the streamed tier costs, made once at the Voice's
// start, and it is why the resident tier exists for everything short.
func newPCMRing(frames, channels int) *pcmRing {
	capacity := uint64(1)
	for capacity < uint64(frames) {
		capacity <<= 1
	}
	r := &pcmRing{
		samples:  make([]float32, capacity*uint64(channels)),
		frames:   capacity,
		mask:     capacity - 1,
		channels: channels,
	}
	r.total.Store(unknownLength)
	return r
}

// at reads one channel of one frame. It is the device thread's only way into
// the ring's samples, and the caller owes the check that the frame is one the
// read-ahead has written: held returns that.
func (r *pcmRing) at(frame uint64, channel, stride int) float32 {
	return r.samples[(frame&r.mask)*uint64(stride)+uint64(channel)]
}

// held is how many frames the read-ahead has produced in total. A frame below
// it and at or above what the Mixer last passed is in the buffer; anything else
// is an underrun.
func (r *pcmRing) held() uint64 { return r.write.Load() }

// passed records the Mixer's playhead, which is the read-ahead's permission to
// overwrite everything below it.
func (r *pcmRing) passed(frame uint64) { r.read.Store(frame) }

// free is how many frames the read-ahead may write without overwriting one the
// Mixer has not passed yet.
//
// The clamp is an underrun that has already happened: the Mixer plays silence
// for a starved slot and its playhead keeps moving, so it can be past frames
// the read-ahead has not produced. That is the Device's own rule one level down
// - a playhead advances whether or not anyone can hear it - and it is what
// keeps an underrun a gap in the sound rather than a permanent drift behind the
// world.
func (r *pcmRing) free() uint64 {
	write, read := r.write.Load(), r.read.Load()
	if read > write {
		read = write
	}
	return r.frames - (write - read)
}

// push writes frames at the end of the ring and then publishes them. The store
// of write is last, so the Mixer never sees a frame number it can reach before
// the samples behind it are there.
func (r *pcmRing) push(in []float32) {
	write := r.write.Load()
	frames := uint64(len(in) / r.channels)
	for at := uint64(0); at < frames; {
		start := (write + at) & r.mask
		run := min(frames-at, r.frames-start)
		copy(r.samples[start*uint64(r.channels):], in[at*uint64(r.channels):(at+run)*uint64(r.channels)])
		at += run
	}
	r.write.Store(write + frames)
}

// finish records that the stream has ended after the frames already pushed,
// which is what lets a streamed one-shot end in the Mixer at the sample its
// Clip runs out on rather than being cut by a length nothing knew.
func (r *pcmRing) finish() { r.total.Store(r.write.Load()) }

// length reports the stream's length in frames, or unknownLength while the
// read-ahead is still ahead of it.
func (r *pcmRing) length() uint64 { return r.total.Load() }
