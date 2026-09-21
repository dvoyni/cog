//go:build !js

package internal

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/jfreymuth/oggvorbis"
	"github.com/jfreymuth/vorbis"
)

// This is the streamed tier: a decoder and a goroutine per Voice, filling a
// ring the Mixer copies out of.
//
// It exists because neither tier alone is defensible. A five-minute stereo
// track resident is 101 MiB of float32 against 4.8 MB encoded - 21x - so music
// cannot be resident; and a decoder is 460 microseconds and 137 KB to open, so
// a footstep cannot pay for one to play something whose entire decoded form is
// smaller than the decoder streaming it. The limit between them is
// Config.DecodedClipLimit, and which side a Clip fell on is invisible above the
// seam: both tiers report the same four facts, and a game cannot ask.
//
// Nothing here runs on the device thread, and that is the whole point of it.

const (
	// readAhead is how much audio a Voice's ring holds. At 48 kHz stereo that
	// is a 128 KB buffer per streamed Voice, made once at the Voice's start.
	// It is deep enough that an ordinary scheduling hiccup on either side is
	// inaudible and shallow enough that sixty-four of them is a few megabytes.
	readAhead = 200 * time.Millisecond
	// refillEvery is how long the read-ahead waits when the ring is full. It is
	// a fraction of the read-ahead, so a Voice that has just been drained is
	// refilled long before it runs dry, and it is a sleep rather than a wait on
	// a signal because the thing that would signal is the device thread, which
	// may not take a lock and may not touch a channel.
	refillEvery = 5 * time.Millisecond
	// decodeChunk is how many source frames one decode asks for. Larger is
	// fewer wakeups; smaller is a shorter stretch of work between two checks of
	// whether the Voice is still alive.
	decodeChunk = 4096
)

// source is one decoder over a streamed Clip's encoded bytes, named as an
// interface for the same reason audio is: so that the tier above it is testable
// without the thing underneath. A test's source can count its reads, refuse to
// keep up, or panic outright if it is ever called from the device thread, which
// is how the obligation that nothing decodes there is proved rather than
// asserted.
type source interface {
	// read decodes into dst, interleaved, and reports how many values it wrote.
	// It reports io.EOF at the end of the stream, an io.Reader's contract.
	read(dst []float32) (int, error)
	// seek moves to a frame. The streamed tier reads from an in-memory Blob,
	// which is seekable, so this is sample-exact rather than page-accurate.
	seek(frame int64) error
}

// opener opens a decoder over a Clip's retained bytes and the Setup Prepare
// read from them. A Clip holds its own, which is what lets a test build a
// streamed Clip over samples it generated with no Ogg anywhere in it; such a
// test's opener ignores the Setup, which is nil.
type opener func(encoded assets.Blob, setup *vorbis.Setup) (source, error)

// oggSource is the real decoder: jfreymuth/oggvorbis over a bytes.Reader on the
// bytes the Clip retained. The Blob is a pointer and a length, so the decoder
// reads the same run the asset cache already holds and nothing is copied.
type oggSource struct{ reader *oggvorbis.Reader }

// openOgg opens a Voice's decoder from the Clip's Setup rather than from its
// headers, so what a Voice pays is the decoder's own buffers and the scan for
// the stream's length; the setup header was parsed once, in Prepare, for the
// Clip. The stream's headers are still checked against the Setup, which is
// how a Clip whose bytes and Setup disagree is a refused open rather than
// garbage.
func openOgg(encoded assets.Blob, setup *vorbis.Setup) (source, error) {
	reader, err := oggvorbis.NewReaderWithSetup(bytes.NewReader(encoded.Data()), setup)
	if err != nil {
		return nil, err
	}
	return &oggSource{reader: reader}, nil
}

func (s *oggSource) read(dst []float32) (int, error) { return s.reader.Read(dst) }

func (s *oggSource) seek(frame int64) error { return s.reader.SetPosition(frame) }

// stream is one streamed Voice's read-ahead: the ring the Mixer reads, and the
// goroutine that fills it.
//
// It belongs to the tick. The tick makes one when it stages a start on a
// streamed Clip and halts it when it stages that slot's stop, so a read-ahead
// stops with its Voice however the Voice ended - finished, stolen, released, or
// cut by a despawn - because all four reach the Adapter as the same stop.
//
// The Mixer is handed the ring and never the stream, which is what keeps the
// device thread's half of this file to two atomic counters and a buffer.
type stream struct {
	ring *pcmRing
	stop chan struct{}
	// done is closed when the read-ahead goroutine has returned. Nothing on the
	// tick waits on it - the tick never waits - but a test may, and it is what
	// makes "the read-ahead stopped" an assertion rather than a hope.
	done chan struct{}
	once sync.Once
}

// newStream starts a Voice's read-ahead. The ring is sized here, once, from the
// device rate and the Clip's channel count, and is never reallocated.
//
// offset is where in the Clip the Voice begins, which is also what a Seek and
// what a recovery after a Device loss both arrive as: a start carrying a
// position. The decoder open and the seek that follow it happen on the
// goroutine this spawns, so neither the tick nor the device thread ever pays
// for them, and sixty-four Voices restarting at once after a reattach is
// sixty-four goroutines' work rather than a stall on the thread that must not.
func newStream(clip *clipData, offset time.Duration, loop bool) *stream {
	s := &stream{
		ring: newPCMRing(int(readAhead.Seconds()*float64(clip.rate)), clip.channels),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go s.fill(clip, offset, loop)
	return s
}

// halt stops the read-ahead. It never blocks, and calling it twice is calling
// it once: a slot restarted in the same tick it was stopped in halts the old
// stream on the stop and again when the start replaces it.
func (s *stream) halt() {
	s.once.Do(func() { close(s.stop) })
}

// halted reports whether the read-ahead has been told to stop.
func (s *stream) halted() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// fill is the read-ahead itself: open a decoder, seek to where the Voice
// begins, and keep the ring ahead of the Mixer until the Clip ends or the Voice
// does.
//
// A decoder that will not open ends the Voice rather than reporting anything. A
// Clip failure is terminal and is reported once, at Prepare, where a Clip that
// could not be read is refused outright; a decoder that fails afterwards is a
// Clip that parsed, was installed, and has been playing, and the game has
// already been told everything it is owed about it. What it gets is silence and
// an ending, which is what it would get from any Voice that ran out.
func (s *stream) fill(clip *clipData, offset time.Duration, loop bool) {
	defer close(s.done)

	decoder, err := clip.open(clip.encoded, clip.setup)
	if err != nil {
		s.ring.finish()
		return
	}
	loopStart, loopEnd := clip.sourceLoopBounds()
	if !loop || loopEnd <= loopStart {
		loop, loopEnd = false, clip.sourceFrames
	}
	at := clip.sourceFrame(offset)
	if at != 0 {
		if err := decoder.seek(at); err != nil {
			s.ring.finish()
			return
		}
	}

	var converter *resampler
	room := decodeChunk
	if clip.filter != nil {
		converter = newResampler(clip.filter, clip.channels)
		room = int(float64(decodeChunk)*clip.filter.ratio) + 2
	}
	decoded := make([]float32, decodeChunk*clip.channels)
	converted := make([]float32, room*clip.channels)

	for !s.halted() {
		if s.ring.free() < uint64(room) {
			if !s.waiting() {
				return
			}
			continue
		}

		want := int64(decodeChunk)
		if remaining := loopEnd - at; remaining < want {
			want = remaining
		}
		var read int
		var failed error
		if want > 0 {
			read, failed = decoder.read(decoded[:want*int64(clip.channels)])
			at += int64(read / clip.channels)
		}
		if read > 0 {
			s.convert(converter, decoded[:read], converted, false)
			continue
		}

		// The end of the span: the Clip's, or the Loop Region's. A looping
		// Voice seeks back and keeps feeding the same conversion, so the filter
		// window reaches across the wrap and the loop is gapless in the
		// resampler as well as in the Mixer; a one-shot drains what the window
		// still holds and publishes the length, which is what ends the Voice.
		if loop {
			if err := decoder.seek(loopStart); err != nil {
				loop = false
				continue
			}
			at = loopStart
			continue
		}
		if failed != nil && failed != io.EOF {
			// A decode that failed part way through is the same as one that
			// ran out: the Voice ends where the samples stopped.
			s.ring.finish()
			return
		}
		s.convert(converter, nil, converted, true)
		s.ring.finish()
		return
	}
}

// convert pushes decoded input into the ring, resampling it on the way when the
// Clip's rate is not the device's. This is the expensive resampler, running on
// the read-ahead goroutine and with exactly the filter Prepare uses for a
// resident Clip, which is what keeps a Clip from sounding different for having
// been long enough to stream.
func (s *stream) convert(converter *resampler, in, scratch []float32, eof bool) {
	if converter == nil {
		if len(in) > 0 {
			s.ring.push(in)
		}
		return
	}
	converter.feed(in)
	for {
		frames := converter.drain(scratch, eof)
		if frames == 0 {
			return
		}
		s.ring.push(scratch[:frames*converter.channels])
		if !eof {
			return
		}
	}
}

// waiting sleeps out one refill interval, or reports false the moment the Voice
// is stopped.
func (s *stream) waiting() bool {
	timer := time.NewTimer(refillEvery)
	defer timer.Stop()
	select {
	case <-s.stop:
		return false
	case <-timer.C:
		return true
	}
}
