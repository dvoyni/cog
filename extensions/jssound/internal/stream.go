//go:build js

package internal

import (
	"bytes"
	"io"
	"sync"
	"syscall/js"
	"time"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/jfreymuth/oggvorbis"
)

// This is the streamed tier: a decoder and a goroutine per Voice, producing
// AudioBuffers a chunk at a time, which chain.go schedules back to back on the
// context clock.
//
// It exists because neither tier alone is defensible. A five-minute stereo
// track held resident is 101 MiB of float32 against 4.8 MB encoded - 21x - and
// on a phone that is the whole page's budget; and a streamed Voice costs a
// decoder, a resampler and a queue of buffers, so a footstep must not pay for
// one to play something whose entire decoded form is smaller than the machinery
// streaming it. The limit between them is Config.DecodedClipLimit, and which
// side a Clip fell on is invisible above the seam: both tiers report the same
// four facts, and a game cannot ask.
//
// # It is not a media element, and the ticket says why
//
// An HTMLMediaElement was checked and refused for a Clip. Chromium and WebKit
// implement its loop as a seek, so a looping track is not gapless and the one
// promise this file exists to keep is lost outright; WebKit's autoplay gesture
// gate is per element, so every track would need its own gesture rather than
// the one the context already has; and the element runs on its own clock rather
// than the AudioContext's, so a seek cannot be landed on a context time and two
// Voices could not be started together. Nothing in this Extension constructs
// one, and no Blob of encoded audio is handed to the page.
//
// # Where the chunks come from, and why they are at the context rate
//
// The decode is jfreymuth/oggvorbis in wasm, reading the Clip's own bytes: it
// demuxes the Ogg pages and decodes the Vorbis packets in Go, a few thousand
// source frames at a time, and the frames go through one resampler that spans
// every chunk and the loop wrap. What reaches the browser is an AudioBuffer
// already at the context rate whose length is a whole number of context frames,
// so a chunk boundary is an integer frame boundary in the context's own
// timeline - which is the only place a seam is free. resample.go says what goes
// wrong if it is not.
//
// WebCodecs AudioDecoder("vorbis"), which would put the decode on the browser's
// own thread where Chromium has one, is not here. It needs the Ogg packets
// handed over raw with the three Vorbis setup headers as the decoder's
// description, in a byte layout no specification this Extension can test
// against pins down, and Safari has no Vorbis in WebCodecs at all - so it would
// be an unverifiable second route behind a capability probe that already has a
// verified first one. It is a later ticket, and the shape here is ready for it:
// a streamReader is an interface, and the route would be a second opener.
//
// Nothing here runs on the audio thread, which is true of this whole Extension:
// the browser owns that thread on the far side of the node graph, and Go cannot
// reach it.

const (
	// decodeChunk is how many source frames one decode asks the decoder for.
	// Larger is fewer wakeups; smaller is a shorter stretch of work between two
	// checks of whether the Voice is still alive.
	decodeChunk = 4096
	// chunkFrames is how many context-rate frames one scheduled AudioBuffer
	// holds: about 85 ms at 48 kHz. It is the unit a seam happens at, so it is
	// long enough that seams are rare and short enough that a Voice starting,
	// seeking or being stolen wastes at most that much decoded audio.
	chunkFrames = 4096
	// streamAhead is how much audio a streamed Voice keeps produced and
	// scheduled in front of the context clock. It is the worst main-thread
	// stall this Adapter promises to survive, and a second is generous against
	// the ~100 ms a garbage collection or a slow frame costs a wasm page -
	// deliberately so, because the failure it prevents is a chunk whose `when`
	// has already passed, which Web Audio starts immediately and which would be
	// a click at a seam rather than a late start.
	streamAhead = time.Second
	// refillEvery is how long the decode goroutine waits when it is far enough
	// ahead. It is a sleep rather than a wait on a signal because the thing
	// that would signal is sound's flush, and a flush must never block on a
	// decoder.
	refillEvery = 5 * time.Millisecond
)

// streamSource is one decoder over a streamed Clip's encoded bytes, named as an
// interface so that the tier above it is testable without the thing underneath.
// A test's source can produce a ramp whose value is its own frame number, which
// is how "no repeated or dropped frame at the loop point" becomes an assertion
// about integers rather than about floats.
type streamSource interface {
	// read decodes into dst, interleaved, and reports how many values it wrote.
	// It reports io.EOF at the end of the stream, an io.Reader's contract.
	read(dst []float32) (int, error)
	// seek moves to a source frame. The bytes are an in-memory Blob, which is
	// seekable, so this is sample-exact rather than page-accurate.
	seek(frame int64) error
}

// opener opens a decoder over a Clip's retained bytes.
type opener func(encoded assets.Blob) (streamSource, error)

// oggSource is the real decoder: jfreymuth/oggvorbis over a bytes.Reader on the
// bytes the Clip retained. A Blob is a pointer and a length, so the decoder
// reads the same run the asset Library already holds and nothing is copied.
type oggSource struct{ reader *oggvorbis.Reader }

func openOgg(encoded assets.Blob) (streamSource, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, err
	}
	return &oggSource{reader: reader}, nil
}

func (s *oggSource) read(dst []float32) (int, error) { return s.reader.Read(dst) }

func (s *oggSource) seek(frame int64) error { return s.reader.SetPosition(frame) }

// chunk is one scheduled unit: an AudioBuffer at the context rate, and where its
// first frame sits in the run's own output-frame numbering.
//
// The frame number is absolute within the run rather than a time, because a time
// depends on the anchor and the rate and both of those move - a rate change
// re-places every chunk that has not finished, and it re-places them from these
// numbers.
type chunk struct {
	buffer js.Value
	at     int64
	frames int
}

// streamer is one streamed Voice's decode-ahead: the goroutine that decodes and
// converts, and the queue of finished chunks the flush takes.
//
// It is the same bargain otosound's pcmRing makes, one side of the seam over.
// Two counters and a slice under one mutex are the whole of the
// synchronisation: the goroutine produces while it is less than streamAhead in
// front of the playhead, the flush says where the playhead has reached and takes
// whatever is ready. Neither ever waits for the other.
//
// A mutex rather than otosound's two atomics because there is no device thread
// here to keep lock-free, and because what crosses is a slice of js.Values
// rather than a run of float32 in a fixed buffer.
type streamer struct {
	mu sync.Mutex
	// ready is produced and not yet scheduled.
	ready []chunk
	// produced is how many output frames this run has decoded in total, and
	// passed is how many the context clock has gone by. Their difference is the
	// read-ahead, which is what throttles the goroutine.
	produced, passed int64
	// ended is a one-shot that has reached the end of its Clip, or a decoder
	// that failed part way through. Either way there is nothing more to
	// schedule, and the Voice ends on sound's own playhead exactly as a resident
	// one whose buffer ran out does.
	ended bool

	lookahead int64
	stop      chan struct{}
	// done is closed when the goroutine has returned. Nothing on the flush waits
	// on it - a flush never waits - but a test may, and it is what makes "the
	// read-ahead stopped with its Voice" an assertion rather than a hope.
	done chan struct{}
	once sync.Once
}

// newStreamer starts a Voice's decode-ahead at a source frame.
//
// The decoder open and the seek happen on the goroutine this spawns, so neither
// sound's flush nor the browser's audio thread pays for them - which matters
// more here than it does on the desktop, because the flush this would stall is
// the same main thread that is also compositing the frame.
func newStreamer(audio *webAudio, clip *clipData, ctxRate int, from int64, loop bool) *streamer {
	s := &streamer{
		lookahead: int64(streamAhead.Seconds() * float64(ctxRate)),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	if s.lookahead < chunkFrames {
		s.lookahead = chunkFrames
	}
	go s.fill(audio, clip, ctxRate, from, loop)
	return s
}

// halt stops the decode-ahead. It never blocks, and calling it twice is calling
// it once: a slot restarted in the same tick it was stopped in halts the old
// streamer on the stop and again when the start replaces it.
func (s *streamer) halt() { s.once.Do(func() { close(s.stop) }) }

func (s *streamer) halted() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// advance records where the context clock has reached, in this run's output
// frames. It is the goroutine's permission to decode further.
func (s *streamer) advance(frame int64) {
	s.mu.Lock()
	if frame > s.passed {
		s.passed = frame
	}
	s.mu.Unlock()
}

// take hands the flush every chunk finished since the last call.
func (s *streamer) take() []chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ready) == 0 {
		return nil
	}
	taken := s.ready
	s.ready = nil
	return taken
}

// finished reports whether the decode-ahead has reached the end of the Clip and
// produced everything it ever will.
func (s *streamer) finished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended && len(s.ready) == 0
}

// room reports whether the goroutine is less than streamAhead in front of the
// playhead, and so may decode another chunk.
func (s *streamer) room() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.produced-s.passed < s.lookahead
}

func (s *streamer) publish(one chunk) {
	s.mu.Lock()
	s.ready = append(s.ready, one)
	s.produced += int64(one.frames)
	s.mu.Unlock()
}

func (s *streamer) finish() {
	s.mu.Lock()
	s.ended = true
	s.mu.Unlock()
}

// waiting sleeps out one refill interval, or reports false the moment the Voice
// is stopped.
func (s *streamer) waiting() bool {
	timer := time.NewTimer(refillEvery)
	defer timer.Stop()
	select {
	case <-s.stop:
		return false
	case <-timer.C:
		return true
	}
}

// fill is the decode-ahead itself: open a decoder, seek to where the Voice
// begins, and keep chunks coming until the Clip ends or the Voice does.
//
// A decoder that will not open ends the Voice rather than reporting anything. A
// Clip failure is terminal and is reported once, at Prepare, where a Clip that
// could not be read is refused outright; a decoder that fails afterwards is a
// Clip that parsed, installed and has been playing, and the game has already
// been told everything it is owed about it. What it gets is silence and an
// ending, which is what it would get from any Voice that ran out.
func (s *streamer) fill(audio *webAudio, clip *clipData, ctxRate int, from int64, loop bool) {
	defer close(s.done)

	decoder, err := clip.open(clip.encoded)
	if err != nil {
		s.finish()
		return
	}
	loopStart, loopEnd := clip.sourceLoopBounds()
	if !loop || loopEnd <= loopStart {
		loop, loopEnd = false, clip.frames
	}
	if from >= loopEnd && !loop {
		s.finish()
		return
	}
	if from != 0 {
		if err := decoder.seek(from); err != nil {
			s.finish()
			return
		}
	}

	reader := &streamReader{
		decoder:   decoder,
		channels:  clip.channels,
		loop:      loop,
		loopStart: loopStart,
		loopEnd:   loopEnd,
		at:        from,
		decoded:   make([]float32, decodeChunk*clip.channels),
	}
	if clip.filter != nil {
		reader.converted = newResampler(clip.filter, clip.channels)
		reader.room = make([]float32, (decodeChunk*2+2)*clip.channels)
	}
	out := make([]float32, chunkFrames*clip.channels)

	var at int64
	for !s.halted() {
		if !s.room() {
			if !s.waiting() {
				return
			}
			continue
		}
		frames, done := reader.pull(out)
		if frames > 0 {
			buffer, err := audio.newBuffer(clip.channels, frames, ctxRate)
			if err != nil {
				s.finish()
				return
			}
			for channel := range clip.channels {
				copyToChannel(buffer, channel, clip.channels, out[:frames*clip.channels])
			}
			s.publish(chunk{buffer: buffer, at: at, frames: frames})
			at += int64(frames)
		}
		if done {
			s.finish()
			return
		}
	}
}

// streamReader turns a decoder into a run of output frames at the context rate,
// wrapping at the Loop Region and converting through one resampler that never
// restarts.
//
// The wrap is in the source's own frames, before the conversion rather than
// after it, which is the whole of the gapless promise: the resampler's window
// reaches across the loop join exactly as it reaches across a chunk boundary, so
// a looping Voice's samples are what one uninterrupted conversion of the looped
// source would have been - no frame repeated, none dropped.
type streamReader struct {
	decoder   streamSource
	converted *resampler
	channels  int
	loop      bool
	// loopStart and loopEnd bound the span, in source frames, that a looping
	// Voice repeats between; loopEnd is the Clip's own end for a one-shot.
	loopStart, loopEnd int64
	// at is where the decoder is, in source frames.
	at int64

	// decoded is the decoder's scratch, room is the resampler's, and pending is
	// the output frames converted and not yet copied into a chunk. All three are
	// made once, so a Voice playing for five minutes allocates for none of them
	// again.
	decoded, room, pending []float32
	held                   []float32
	// drained is set once the resampler has been told the input has ended and
	// has given back the last of what its window still held.
	drained bool
	// wrapped is set by a loop seek and cleared by the next frame actually
	// decoded, so that two wraps with nothing between them end the Voice instead
	// of spinning.
	wrapped bool
	// ended is set once there is nothing more to produce, ever.
	ended bool
}

// pull fills dst with output frames and reports how many it wrote and whether
// the stream has ended. A short write that is not an ending is a chunk cut
// early, which the caller schedules exactly as it schedules a full one: chunks
// are placed by frame count, so a short one moves the next one closer rather
// than leaving a hole.
func (r *streamReader) pull(dst []float32) (int, bool) {
	frames := len(dst) / r.channels
	wrote := 0
	for wrote < frames {
		if len(r.pending) > 0 {
			take := min(frames-wrote, len(r.pending)/r.channels)
			copy(dst[wrote*r.channels:], r.pending[:take*r.channels])
			r.pending = r.pending[take*r.channels:]
			wrote += take
			continue
		}
		if r.ended {
			return wrote, true
		}
		r.refill()
	}
	return wrote, false
}

// refill decodes the next span of source frames, wrapping at the loop point, and
// converts it into pending.
func (r *streamReader) refill() {
	want := int64(decodeChunk)
	if remaining := r.loopEnd - r.at; remaining < want {
		want = remaining
	}
	var read int
	var failed error
	if want > 0 {
		read, failed = r.decoder.read(r.decoded[:want*int64(r.channels)])
		r.at += int64(read / r.channels)
	}
	if read > 0 {
		// read is a count of values rather than of frames, which is what an
		// io.Reader over interleaved samples reports and what the decoder's own
		// signature says.
		r.wrapped = false
		r.pending = r.convert(r.decoded[:read], false)
		return
	}

	// The end of the span: the Clip's, or the Loop Region's. A looping Voice
	// seeks back and keeps feeding the same conversion; a one-shot drains what
	// the filter window still holds and then has nothing more.
	//
	// A wrap that produced no frames before wrapping again is a Loop Region over
	// a span the decoder will not give anything for, and it ends the Voice
	// rather than spinning: a decoder is allowed to read nothing once, and a
	// second time in a row with no progress between them is not a loop.
	if r.loop && !r.wrapped && (failed == nil || failed == io.EOF) {
		if err := r.decoder.seek(r.loopStart); err != nil {
			r.loop = false
		} else {
			r.at, r.wrapped = r.loopStart, true
			return
		}
	}
	if r.converted == nil || r.drained {
		r.ended = true
		return
	}
	r.drained = true
	r.pending = r.convert(nil, true)
	if len(r.pending) == 0 {
		r.ended = true
	}
}

// convert pushes decoded input through the resampler and returns the output
// frames it produced, or the input itself when the file is already at the
// context rate.
//
// The copy in the unconverted case is not avoidable and not wasted: decoded is
// the decoder's scratch and is overwritten by the next read, while pending is
// handed out across calls.
func (r *streamReader) convert(in []float32, eof bool) []float32 {
	out := r.held[:0]
	if r.converted == nil {
		r.held = append(out, in...)
		return r.held
	}
	r.converted.feed(in)
	for {
		frames := r.converted.drain(r.room, eof)
		if frames == 0 {
			break
		}
		out = append(out, r.room[:frames*r.channels]...)
	}
	r.held = out
	return r.held
}
