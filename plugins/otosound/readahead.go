//go:build !js

package otosound

import (
	"io"
	"sync"
	"time"
)

// readAheadChunkFrames is how many source frames a read-ahead decodes at a
// time: fewer wakeups the larger it is, and a shorter stretch of work
// between two checks of whether its voice still plays the smaller.
const readAheadChunkFrames = 4096

// readAhead is one streamed voice's read-ahead: the ring the mixer reads, and
// the goroutine that decodes ahead into it, opening a decoder over the
// clip's bytes, seeking it to the voice's offset and converting what it
// decodes to the rate the mixer produces. No clip's bytes are decoded on the
// thread that fills the device buffer: it only ever copies out of rings.
//
// It is the flush side's: Emit opens one when it stages a start on a
// streamed clip and halts it on the slot's stop, or on the start that
// replaces the slot, so a read-ahead stops with its voice however the voice
// ended, finished, stolen, released or cut, since each reaches otosound as
// the same stop. The mixer is handed the ring and never the read-ahead.
type readAhead struct {
	// ring is what the read-ahead fills and the mixer reads.
	ring *readAheadRing
	// clip is the streamed clip it decodes, offset where in it the voice
	// starts, and loop whether it wraps at the clip's loop region.
	clip   *preparedClip
	offset time.Duration
	loop   bool
	// chunkFrames is the most frames one step of the read-ahead pushes: a
	// chunk decoded, or what the conversion drains of one.
	chunkFrames int64
	// halted is closed by halt, once, which haltOnce guards, and done when
	// the goroutine has returned.
	halted   chan struct{}
	haltOnce sync.Once
	done     chan struct{}
}

// newReadAhead returns the read-ahead of a voice starting on clip at offset,
// looping when loop is set, its ring sized for ringFrames frames at the rate
// the mixer produces, and never below one step's frames, so a step always
// fits. It allocates the ring, on the flush side, which is the streamed
// tier's cost per voice; its fill runs on a goroutine of the caller's.
func newReadAhead(clip *preparedClip, offset time.Duration, loop bool, ringFrames int) *readAhead {
	chunkFrames := int64(readAheadChunkFrames)
	if clip.filter != nil {
		// A chunk converts to at most one frame more than the ratio says,
		// the window having reached one more output frame's end.
		chunkFrames = clip.filter.outputFrameCount(readAheadChunkFrames) + 1
	}
	return &readAhead{
		ring:        newReadAheadRing(max(ringFrames, int(chunkFrames)), clip.channels),
		clip:        clip,
		offset:      offset,
		loop:        loop,
		chunkFrames: chunkFrames,
		halted:      make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// halt stops the read-ahead. It never blocks, and halting twice is halting
// once. It does not free the ring: the mixer may still be ramping the voice
// to silence out of frames the ring holds, and the ring goes when the voice
// table drops it.
func (r *readAhead) halt() { r.haltOnce.Do(func() { close(r.halted) }) }

// fill is the read-ahead itself, run on its own goroutine until the clip
// ends or the voice does: it opens a decoder, seeks it to the voice's offset
// in source frames, and keeps the ring ahead of the mixer, sleeping
// refillWait whenever the ring has less room than half a step. It sleeps rather than
// waits on a signal, since what would signal is the device thread, which may
// not touch a channel.
//
// A looping voice seeks back to its span's start on reaching its end, and
// feeds the same conversion across the wrap, so the filter's window reaches
// across it and the loop is gapless in the resampler as well as in the
// mixer; it never publishes a length, so it never ends by itself. A one-shot
// drains what the window still holds at the clip's end and publishes the
// length, which ends the voice in the mixer on the frame its clip runs out
// on.
//
// A decoder that will not open, or fails mid-clip, ends the voice and
// reports nothing: the clip parsed at its prepare and has been playing, and
// the game has been told everything it is owed. It gets silence and an
// ending, as any voice that ran out does.
func (r *readAhead) fill(refillWait time.Duration) {
	defer close(r.done)
	if r.stream(refillWait) {
		r.ring.length.Store(r.ring.produced.Load())
	}
}

// stream runs fill's decoding until the stream ends, answering true, or the
// read-ahead is halted, answering false: every way a voice's stream can end,
// the clip's end, a decoder that will not open and a decode or a seek that
// fails, ends it on the frames already produced.
func (r *readAhead) stream(refillWait time.Duration) bool {
	clip := r.clip
	channels := int64(clip.channels)
	decoder, err := clip.openDecoder(clip.encoded)
	if err != nil {
		return true
	}
	sourceRate := float64(clip.sampleRate)
	spanStart := int64(0)
	spanEnd := clip.frames
	if r.loop {
		start, end := clip.loopBounds(sourceRate, float64(clip.frames))
		spanStart = int64(start)
		spanEnd = int64(end)
	}
	// An offset past the clip's end seeks to its last frame.
	position := max(0, min(clip.frames-1, int64(r.offset.Seconds()*sourceRate)))
	if position != 0 && decoder.seek(position) != nil {
		return true
	}
	var converter *resampler
	if clip.filter != nil {
		converter = newResampler(clip.filter, clip.channels)
	}
	decodedSamples := make([]float32, readAheadChunkFrames*channels)
	convertedSamples := make([]float32, r.chunkFrames*channels)
	refillTimer := time.NewTimer(refillWait)
	defer refillTimer.Stop()
	// draining is whether the conversion may hold more than its last drain
	// took, which the next step drains before decoding more; inputEnded is a
	// one-shot past its clip's end, draining the window's tail.
	draining := false
	inputEnded := false
	for {
		select {
		case <-r.halted:
			return false
		default:
		}
		// A step pushes what fits, up to a step's frames, and waits for half
		// a step's room, so it neither wakes for a handful of frames nor
		// waits on the mixer passing a whole ring a step long before it can
		// push the last few frames of a clip.
		roomFrames := min(int64(r.ring.roomFrames()), r.chunkFrames)
		if roomFrames < r.chunkFrames/2 {
			refillTimer.Reset(refillWait)
			select {
			case <-r.halted:
				return false
			case <-refillTimer.C:
			}
			continue
		}
		if draining || inputEnded {
			writtenFrames := int64(converter.drain(convertedSamples[:roomFrames*channels], inputEnded))
			r.ring.push(convertedSamples[:writtenFrames*channels])
			draining = writtenFrames == roomFrames
			if inputEnded && !draining {
				return true
			}
			continue
		}
		if position < spanEnd {
			wantFrames := min(readAheadChunkFrames, spanEnd-position)
			if converter == nil {
				wantFrames = min(wantFrames, roomFrames)
			}
			readCount, err := decoder.read(decodedSamples[:wantFrames*channels])
			if readCount > 0 {
				position += int64(readCount) / channels
				if converter == nil {
					r.ring.push(decodedSamples[:readCount])
				} else {
					converter.feed(decodedSamples[:readCount])
					draining = true
				}
				continue
			}
			if err != io.EOF {
				// A decode that failed, or made no progress, part way
				// through ends the voice where its frames stopped.
				return true
			}
		}
		// The span's end: the clip's, or its loop region's.
		if r.loop && spanStart < position {
			if decoder.seek(spanStart) != nil {
				return true
			}
			position = spanStart
			continue
		}
		if converter == nil {
			return true
		}
		inputEnded = true
	}
}
