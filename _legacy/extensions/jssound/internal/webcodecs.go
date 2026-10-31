//go:build js

package internal

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
	"syscall/js"

	"github.com/dvoyni/cog/libs/assets"
)

// This is the streamed tier's second decoder: WebCodecs AudioDecoder("vorbis"),
// which decodes on a thread of the browser's own rather than in wasm on the
// thread that is also compositing the frame.
//
// It is the half of the ticket the Go decoder could not be: demuxing in Go is
// what oggpackets.go does either way, but jfreymuth/oggvorbis decodes in wasm,
// and Go's wasm target is single-threaded, so a streamed Voice costs real
// main-thread CPU for as long as it plays. Spread across event-loop turns by the
// per-Voice goroutine it is never one stall, but it is never free either. An
// AudioDecoder moves that work off the page entirely: what crosses the boundary
// is a packet in and a block of samples out.
//
// # Why it is a second route and not the route
//
// Safari has no Vorbis in WebCodecs at all - its AudioDecoder reaches the same
// operating system codec that has no Ogg - so the Go decoder is not replaced,
// it is what a browser without this falls back to. The gate is
// AudioDecoder.isConfigSupported, asked once at init with the embedded
// micro-clip's own headers, because that is the only question with a yes and a
// no: a user-agent string cannot distinguish a Safari that has the codec from
// one that does not, which is the same reason probe.go decodes rather than
// sniffs.
//
// A Clip that then will not demux falls back per-Clip as well, so a file this
// walker mis-reads is played by the decoder that parses it properly rather than
// refused.
//
// To force the fallback in a browser that has the codec - which is how the two
// routes are compared by ear in one page - delete AudioDecoder from the global
// object before the Engine starts. The probe then answers no and every streamed
// Clip decodes in Go, with nothing else about the Adapter changed.
//
// # Position is counted, never read back
//
// An AudioData carries a timestamp, and this ignores it. Where a decoded frame
// belongs is worked out by counting frames from a cold decoder, because a cold
// Vorbis decoder fed from the stream's first audio packet emits exactly the
// stream from its first frame - which is the same property decodeAudioData
// rests on, and which was measured: the fixture's 48704 frames of granule are
// 48704 frames out of both.
//
// That is also why a seek restarts rather than skips. WebCodecs has no seek, and
// the alternatives - mapping a page's granule position onto an output, or
// trusting a decoder to hand a chunk's timestamp back on the AudioData it
// produced - are both assumptions about an implementation rather than about the
// format. Restarting and discarding assumes only the property above. It costs a
// decode of everything before the target, off the main thread, and for a Clip
// whose Loop Region begins minutes in that is the one place this route is
// slower than the Go one; a page-granule skip is the tidier answer if that ever
// turns up in a real game.
const (
	// codecsAhead is how many packets are fed to the decoder before this waits
	// for output rather than feeding more. It is a pipeline depth: too shallow
	// and the decoder idles between packets, too deep and a seek throws away
	// more work than it had to. A Vorbis packet is about a thousand frames, so
	// this is roughly three quarters of a second of audio in flight.
	codecsAhead = 32
	// nominalPacketFrames is what a chunk's timestamp is computed from. Vorbis
	// packets are not all one length, so this is a nominal spacing rather than
	// a measurement - which is honest, because nothing reads these timestamps
	// back. They exist because EncodedAudioChunk requires one and because a
	// decoder is entitled to expect them to increase.
	nominalPacketFrames = 1024
)

// codecsSupport is the answer to the second question jssound asks the browser
// about itself: does its AudioDecoder decode Vorbis?
type codecsSupport int

const (
	// codecsUnknown is before the probe has settled. A prepare that arrives
	// here waits with the ones waiting on the Ogg probe, for the same reason: a
	// Clip sent down a route the probe would have changed is the probe doing
	// nothing.
	codecsUnknown codecsSupport = iota
	// codecsPresent is a browser whose AudioDecoder took a Vorbis config.
	codecsPresent
	// codecsAbsent is a browser with no WebCodecs, or with WebCodecs and no
	// Vorbis in it, which is every Safari.
	codecsAbsent
)

// probeCodecs asks whether this browser's AudioDecoder will take a Vorbis
// configuration, and calls settled with the answer exactly once.
//
// The configuration it asks with is the embedded micro-clip's, description and
// all, rather than a codec string alone: a browser is entitled to refuse a
// config for its description rather than for its codec, and the description is
// the part of this route that had never been tried against a real decoder. So
// the probe asks the question the streamed route will ask.
//
// It is the codec that is being probed and not a Clip. A browser that decodes
// Vorbis at 44.1 kHz stereo decodes it at every rate and channel count Vorbis
// has, and asking per Clip would put a promise in the middle of sound's flush.
func probeCodecs(settled func(codecsSupport)) {
	constructor := js.Global().Get("AudioDecoder")
	if !constructor.Truthy() || !constructor.Get("isConfigSupported").Truthy() {
		settled(codecsAbsent)
		return
	}
	stream, err := demuxVorbis(probeClip)
	if err != nil {
		settled(codecsAbsent)
		return
	}

	var ok, failed js.Func
	var done bool
	answer := func(support codecsSupport) {
		if done {
			return
		}
		done = true
		// Released from inside the call the browser is making, which is the
		// earliest a js.Func handle can be given back at all - the same shape
		// webaudio.go's decode uses for its two callbacks.
		ok.Release()
		failed.Release()
		settled(support)
	}
	ok = js.FuncOf(func(_ js.Value, args []js.Value) any {
		supported := len(args) > 0 && args[0].Get("supported").Truthy()
		if supported {
			answer(codecsPresent)
		} else {
			answer(codecsAbsent)
		}
		return js.Undefined()
	})
	failed = js.FuncOf(func(js.Value, []js.Value) any {
		answer(codecsAbsent)
		return js.Undefined()
	})

	defer func() {
		if caught := recover(); caught != nil {
			answer(codecsAbsent)
		}
	}()
	constructor.Call("isConfigSupported", stream.config()).
		Call("then", ok).Call("catch", failed)
}

// config is the AudioDecoderConfig this stream is decoded under.
func (v *vorbisStream) config() js.Value {
	config := js.Global().Get("Object").New()
	config.Set("codec", "vorbis")
	config.Set("sampleRate", v.sampleRate)
	config.Set("numberOfChannels", v.channels)
	config.Set("description", toBytes(v.description))
	return config
}

// codecsSource is one streamed Voice's AudioDecoder, worn as the streamSource
// the tier above already knows how to drive.
//
// Everything about it is shaped by one fact: the decoder answers from the event
// loop and the tier above asks synchronously. So read feeds packets until there
// is output to hand back and otherwise blocks on a channel, which in Go's wasm
// runtime is the goroutine yielding to the page - the decode-ahead goroutine
// parking is exactly what lets the browser's callback run at all. Nothing here
// ever blocks sound's flush, which never touches this type.
type codecsSource struct {
	stream   *vorbisStream
	channels int
	decoder  js.Value
	onOutput js.Func
	onError  js.Func

	// next and flushed belong to the reading goroutine alone: it is the only
	// thing that feeds the decoder, so they need no lock.
	next    int
	flushed bool

	// mu guards what the browser's callbacks write and the reading goroutine
	// reads. The two never run at once - a page runs Go and JS on one thread -
	// but they interleave wherever a goroutine parks, which is once per read.
	mu sync.Mutex
	// ready is decoded output not yet handed over, oldest first, interleaved.
	ready [][]float32
	// skip is how many frames a seek still has to throw away before the output
	// is the audio that was asked for.
	skip int64
	// inFlight is packets fed whose output has not come back, which is what
	// bounds how far ahead this feeds.
	inFlight int
	failed   error
	// drained is set when a flush has resolved: the decoder has emitted
	// everything it will ever emit for what it was fed.
	drained bool
	closed  bool

	// wake carries one token and is how a callback tells the reading goroutine
	// that something changed. A callback must never block, so every send is a
	// send that may be dropped: a dropped one is a token already waiting, and
	// the state it would have announced has been written before it.
	wake chan struct{}
}

// openWebCodecs opens an AudioDecoder over a Clip's retained bytes.
//
// It runs on the goroutine the streamed route spawns, never on sound's flush,
// which is where the demux of a five-minute file belongs.
func openWebCodecs(encoded assets.Blob) (streamSource, error) {
	stream, err := demuxVorbis(encoded.Data())
	if err != nil {
		return nil, err
	}
	source := &codecsSource{
		stream:   stream,
		channels: stream.channels,
		wake:     make(chan struct{}, 1),
	}
	if err := source.configure(); err != nil {
		source.close()
		return nil, err
	}
	return source, nil
}

// openStreamedVorbis is the opener a streamed Clip gets on a browser whose
// AudioDecoder took the probe's Vorbis config: WebCodecs where the Clip demuxes,
// and jfreymuth/oggvorbis where it does not.
//
// The per-Clip fallback is not the same question the probe asked. The probe says
// whether the browser has the codec; this says whether these particular bytes
// came apart into packets, and a file that did not is one the Go decoder should
// be given because it will say what is wrong with it properly.
func openStreamedVorbis(encoded assets.Blob) (streamSource, error) {
	if source, err := openWebCodecs(encoded); err == nil {
		return source, nil
	}
	return openOgg(encoded)
}

// configure constructs the decoder and hands it the stream's configuration.
func (s *codecsSource) configure() (err error) {
	defer func() {
		if caught := recover(); caught != nil {
			err = jsError(caught)
		}
	}()
	handlers := js.Global().Get("Object").New()
	s.onOutput = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			s.received(args[0])
		}
		return js.Undefined()
	})
	s.onError = js.FuncOf(func(_ js.Value, args []js.Value) any {
		s.fail(errorMessage(args))
		return js.Undefined()
	})
	handlers.Set("output", s.onOutput)
	handlers.Set("error", s.onError)
	s.decoder = js.Global().Get("AudioDecoder").New(handlers)
	s.decoder.Call("configure", s.stream.config())
	return nil
}

// read hands back the next decoded frames, feeding the decoder until it has
// some. It is io.Reader's contract over interleaved samples, which is what the
// tier above drives.
func (s *codecsSource) read(dst []float32) (int, error) {
	want := len(dst) / s.channels
	if want == 0 {
		return 0, nil
	}
	for {
		values, err := s.take(dst, want)
		if values > 0 || err != nil {
			return values, err
		}
		if !s.step() {
			<-s.wake
		}
	}
}

// take copies whatever is decoded into dst, and reports the end of the stream
// or the decoder's failure where there is nothing decoded and never will be.
func (s *codecsSource) take(dst []float32, want int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wrote := 0
	for wrote < want && len(s.ready) > 0 {
		head := s.ready[0]
		frames := min(want-wrote, len(head)/s.channels)
		copy(dst[wrote*s.channels:], head[:frames*s.channels])
		wrote += frames
		if rest := head[frames*s.channels:]; len(rest) > 0 {
			s.ready[0] = rest
		} else {
			s.ready = s.ready[1:]
		}
	}
	if wrote > 0 {
		return wrote * s.channels, nil
	}
	if s.failed != nil {
		return 0, s.failed
	}
	if s.drained || s.closed {
		// A closed decoder reads as an ended stream rather than as an error,
		// because the only thing that closes one is the Voice that owned it
		// going away, and a read after that has nothing to report to.
		return 0, io.EOF
	}
	return 0, nil
}

// step asks the decoder for the next thing it can be asked for, and reports
// whether there was one. False means the only thing left to do is wait, and
// there is always something outstanding when it says so: either packets in
// flight, or a flush that has not resolved.
func (s *codecsSource) step() bool {
	s.mu.Lock()
	inFlight, stopped := s.inFlight, s.closed || s.failed != nil
	s.mu.Unlock()
	if stopped {
		return true
	}
	if s.next < len(s.stream.packets) {
		if inFlight >= codecsAhead {
			return false
		}
		s.feed()
		return true
	}
	if !s.flushed {
		s.flush()
		return true
	}
	return false
}

// feed hands the decoder one Vorbis packet as an EncodedAudioChunk.
//
// Every audio packet is a key frame, which is what Vorbis is: a packet depends
// on the window of the one before it for its overlap, but there is no other
// kind of packet to be told apart from, and a decoder given a run of them from
// the start decodes the stream.
func (s *codecsSource) feed() {
	defer func() {
		if caught := recover(); caught != nil {
			s.fail(jsError(caught).Error())
		}
	}()
	index := s.next
	init := js.Global().Get("Object").New()
	init.Set("type", "key")
	init.Set("timestamp", int64(index)*nominalPacketFrames*1e6/int64(s.stream.sampleRate))
	init.Set("data", toBytes(s.stream.packets[index]))
	chunk := js.Global().Get("EncodedAudioChunk").New(init)

	s.mu.Lock()
	s.inFlight++
	s.mu.Unlock()
	s.next++
	s.decoder.Call("decode", chunk)
}

// flush asks the decoder to emit everything it still holds, and is asked for
// exactly once, at the end of the stream.
//
// It is at the end and nowhere else on purpose. A flush is specified to make a
// decoder produce all its outputs, which for a lapped codec means giving up the
// window it would have overlapped the next packet with - so a flush at a chunk
// boundary would put a seam in the middle of a Clip that is meant to have none.
// The tier above asks for chunks; this asks the decoder for nothing but packets
// until the packets run out.
func (s *codecsSource) flush() {
	s.flushed = true
	defer func() {
		if caught := recover(); caught != nil {
			s.fail(jsError(caught).Error())
		}
	}()
	var ok, failed js.Func
	var done bool
	settle := func(err string) {
		if done {
			return
		}
		done = true
		ok.Release()
		failed.Release()
		if err != "" {
			s.fail(err)
			return
		}
		s.mu.Lock()
		s.drained = true
		s.mu.Unlock()
		s.signal()
	}
	ok = js.FuncOf(func(js.Value, []js.Value) any { settle(""); return js.Undefined() })
	failed = js.FuncOf(func(_ js.Value, args []js.Value) any { settle(errorMessage(args)); return js.Undefined() })
	s.decoder.Call("flush").Call("then", ok).Call("catch", failed)
}

// quiesce blocks until the decoder has nothing outstanding: everything it was
// fed has come back, or it has failed. A decoder that was never fed is already
// quiet.
//
// The frames it drains are thrown away by the restart that follows, which is
// not waste worth avoiding - they are the frames past the loop end that were
// decoded ahead, and they were never going to be played.
func (s *codecsSource) quiesce() error {
	if s.next == 0 {
		return nil
	}
	for {
		s.mu.Lock()
		failed, drained := s.failed, s.drained
		s.mu.Unlock()
		switch {
		case failed != nil:
			return failed
		case drained:
			return nil
		case !s.flushed:
			s.flush()
		default:
			<-s.wake
		}
	}
}

// seek moves to a source frame by starting the decoder again and throwing away
// everything before it.
//
// There is no seek in WebCodecs, and the comment at the top of this file says
// why the discard is the honest answer rather than a granule skip. What matters
// to the promise above is that it is exact: the frames handed back after this
// begin at the frame that was asked for, which is what makes a loop wrap land on
// the sample it should and the resampler's window reach across the join.
func (s *codecsSource) seek(frame int64) error {
	if err := s.restart(); err != nil {
		return err
	}
	s.mu.Lock()
	s.skip = frame
	s.mu.Unlock()
	return nil
}

// restart puts the decoder back to a cold one over the same stream.
//
// It waits for the decoder to go quiet before it resets, and that wait is the
// difference between a loop that wraps and one that plays a second of the wrong
// audio at every join. A decode is a request whose answer comes later, so at the
// moment a wrap is decided there are packets in flight from before it; the
// specification says a reset stops their answers arriving, but a route that
// counts frames cannot afford to find out that an implementation disagrees.
// Waiting for a flush to resolve is the same guarantee asked for the other way
// round - it is specified to mean everything has been emitted - and it is one
// flush per loop, which is a thing that happens once a minute rather than once a
// chunk.
func (s *codecsSource) restart() (err error) {
	if err := s.quiesce(); err != nil {
		return err
	}
	defer func() {
		if caught := recover(); caught != nil {
			err = jsError(caught)
		}
	}()
	s.decoder.Call("reset")
	s.decoder.Call("configure", s.stream.config())
	s.next, s.flushed = 0, false
	s.mu.Lock()
	s.ready, s.inFlight, s.skip = nil, 0, 0
	s.failed, s.drained = nil, false
	s.mu.Unlock()
	return nil
}

// close gives the decoder and its two callbacks back.
//
// The decoder is closed before the js.Funcs are released, which is the order
// webaudio.go closes the context in and for the same reason: a released js.Func
// called from JS is a panic in the page rather than an error anything can
// catch. Closing an AudioDecoder is specified to discard its control message
// queue, so nothing is emitted afterwards.
func (s *codecsSource) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed, s.ready = true, nil
	s.mu.Unlock()

	func() {
		defer func() { _ = recover() }()
		if s.decoder.Truthy() {
			s.decoder.Call("close")
		}
	}()
	s.onOutput.Release()
	s.onError.Release()
	s.signal()
}

// received takes one AudioData off the decoder, and is the only place samples
// cross back into Go.
//
// Every AudioData is closed, including the ones a seek throws away. They hold
// decoded audio outside the JS heap's own accounting, and a browser that had to
// wait for a garbage collection to get them back would be a streamed Voice
// costing the memory the tier exists to avoid.
func (s *codecsSource) received(data js.Value) {
	defer func() {
		defer func() { _ = recover() }()
		data.Call("close")
	}()

	frames := data.Get("numberOfFrames").Int()
	s.mu.Lock()
	dropped := int(min(s.skip, int64(frames)))
	s.skip -= int64(dropped)
	s.inFlight--
	closed := s.closed
	s.mu.Unlock()

	if closed || frames <= dropped {
		s.signal()
		return
	}
	samples, err := audioDataFrames(data, s.channels, dropped, frames)
	if err != nil {
		// A browser that will not give these samples up in f32-planar is one
		// this route cannot read at all, and saying so ends the Voice. The
		// alternative - carrying on with nothing - would be a streamed Clip
		// that played silence and reported no reason for it.
		s.fail(err.Error())
		return
	}

	s.mu.Lock()
	if !s.closed && len(samples) > 0 {
		s.ready = append(s.ready, samples)
	}
	s.mu.Unlock()
	s.signal()
}

// fail records the decoder's error and wakes whatever is waiting on it.
//
// A decoder that fails part way through ends the Voice rather than reporting
// anything, which is stream.go's rule: a Clip that parsed, installed and has
// been playing has already been told everything it is owed, and what is left is
// silence and an ending.
func (s *codecsSource) fail(message string) {
	s.mu.Lock()
	if s.failed == nil {
		s.failed = ErrDecodeRefused{Message: message}
	}
	s.mu.Unlock()
	s.signal()
}

// signal wakes the reading goroutine, and never blocks. A send that finds the
// token already there is a wake that has not been consumed yet, and the state it
// would have announced was written before this was called.
func (s *codecsSource) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// audioDataFrames copies one AudioData into interleaved Go samples, dropping the
// first skipped frames a seek is still discarding.
//
// It copies plane by plane in f32-planar, which is the format a browser's audio
// decoder produces, so no conversion is asked of the page. The bytes come over
// as a Uint8Array and are read as little-endian float32, which is
// copyToChannel's journey in webaudio.go run backwards.
func audioDataFrames(data js.Value, channels, from, frames int) (out []float32, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			out, err = nil, jsError(caught)
		}
	}()
	if planes := data.Get("numberOfChannels"); planes.Type() == js.TypeNumber && planes.Int() < channels {
		channels = planes.Int()
	}
	kept := frames - from
	if kept <= 0 || channels <= 0 {
		return nil, nil
	}
	out = make([]float32, kept*channels)
	raw := make([]byte, frames*4)
	view := js.Global().Get("Uint8Array").New(len(raw))
	options := js.Global().Get("Object").New()
	options.Set("format", "f32-planar")
	for channel := range channels {
		options.Set("planeIndex", channel)
		data.Call("copyTo", view, options)
		js.CopyBytesToGo(raw, view)
		for frame := range kept {
			bits := binary.LittleEndian.Uint32(raw[(from+frame)*4:])
			out[frame*channels+channel] = math.Float32frombits(bits)
		}
	}
	return out, nil
}

// toBytes puts Go bytes into a Uint8Array, which is what every WebCodecs
// argument that takes a buffer takes.
func toBytes(data []byte) js.Value {
	view := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(view, data)
	return view
}
