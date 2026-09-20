//go:build js

package internal

import (
	"encoding/binary"
	"syscall/js"
	"testing"
)

// This is the WebCodecs a test gets. node has no AudioDecoder at all, which is
// why the fake is installed only by the tests that want one: every other test in
// this package runs on a page with no WebCodecs, which is the fallback the route
// is gated behind and is worth keeping as the default.
//
// It is a recording fake rather than a stub, for the same two reasons
// fakeaudio_test.go gives. It remembers what it was configured with and what it
// was fed, so an assertion reads the inputs the route actually produced - the
// description bytes, the packets, their order - rather than a count of calls.
// And it answers from the event loop, never inside the call that provoked it: a
// JS callback invoked synchronously from a Go-to-JS call re-enters the wasm
// instance while Go is already running it, which is a hazard the real API never
// presents and which would make the blocking read in webcodecs.go look safe when
// it is not.
//
// # It decodes, and that is what makes it a test
//
// A fake that emitted silence could only say that packets went in. This one
// reads each packet's own payload - a start frame and a frame count, which is
// what syntheticVorbis writes into them - and emits exactly those frames as a
// ramp whose value is the frame's own number. So the assertion at the far end is
// that output frame k carries source frame k, through the real demuxer, the real
// chunk feeding, the real seek-by-restart and the real resampler-less path, and
// a frame repeated or dropped anywhere in it is an integer in the wrong place.
//
// It also models the one thing about a Vorbis decoder that the route's frame
// counting depends on: a cold decoder emits nothing for the first packet it is
// given, because there is no previous window to lap against, and every packet
// after that emits the one before it. That is why decodeAudioData returns the
// granule length and not a window more, and a route that assumed one output per
// packet would be off by a packet from the first seek onwards.
const fakeCodecsSource = `
globalThis.__cogCodecs = {
  supported: true, probes: 0, probedWith: null,
  decoders: 0, configured: [], fed: [], resets: 0, flushes: 0, closes: 0,
  dataMade: 0, dataClosed: 0, failAfter: -1, throwOnConfigure: false,
};
(function () {
  var C = globalThis.__cogCodecs;

  function bytesOf(source) {
    if (!source) { return new Uint8Array(0); }
    if (source instanceof Uint8Array) { return new Uint8Array(source); }
    return new Uint8Array(source.buffer || source);
  }

  function digest(data) {
    var sum = 0;
    for (var i = 0; i < data.length; i++) { sum = (sum * 31 + data[i]) >>> 0; }
    return sum;
  }

  // A synthetic audio packet says what it decodes to in its first eight bytes:
  // the absolute source frame it starts at, and how many frames it holds.
  function describe(data) {
    if (data.length < 8) { return null; }
    var view = new DataView(data.buffer, data.byteOffset, 8);
    return { start: view.getUint32(0, true), frames: view.getUint32(4, true) };
  }

  function audioData(desc, rate) {
    C.dataMade++;
    return {
      format: "f32-planar",
      sampleRate: rate,
      numberOfChannels: 2,
      numberOfFrames: desc.frames,
      duration: desc.frames * 1e6 / rate,
      timestamp: desc.start * 1e6 / rate,
      copyTo: function (destination, options) {
        if (!options || options.format !== "f32-planar") {
          throw new TypeError("the fake decoder only converts to f32-planar");
        }
        var plane = options.planeIndex;
        if (plane !== 0 && plane !== 1) { throw new RangeError("no such plane"); }
        if (destination.byteLength < desc.frames * 4) {
          throw new TypeError("the destination is too small for this plane");
        }
        var view = new Float32Array(destination.buffer, destination.byteOffset, desc.frames);
        for (var i = 0; i < desc.frames; i++) {
          view[i] = (desc.start + i) * (plane === 0 ? 1 : -1);
        }
      },
      close: function () { C.dataClosed++; this.closed = true; },
    };
  }

  globalThis.EncodedAudioChunk = function (init) {
    this.type = init.type;
    this.timestamp = init.timestamp;
    this.data = bytesOf(init.data);
    this.byteLength = this.data.length;
  };

  globalThis.AudioDecoder = function (handlers) {
    var decoder = this;
    var id = ++C.decoders;
    var pending = null;
    var rate = 48000;
    // A reset and a close are both specified to stop the answers to what was
    // already decoded from arriving, and the fake honours that rather than
    // letting a microtask queued before one of them call back after it. Getting
    // this wrong in the fake would hide the ordering the route has to get right
    // at a loop wrap, and would call a released js.Func after a Voice ended.
    var generation = 0;
    decoder.state = "unconfigured";

    function deliver(desc, era) {
      queueMicrotask(function () {
        if (era !== generation || decoder.state === "closed") { return; }
        handlers.output(audioData(desc, rate));
      });
    }

    decoder.configure = function (config) {
      if (C.throwOnConfigure) { throw new TypeError("the fake decoder refuses this configuration"); }
      rate = config.sampleRate;
      C.configured.push({
        decoder: id,
        codec: config.codec,
        sampleRate: config.sampleRate,
        numberOfChannels: config.numberOfChannels,
        descriptionBytes: bytesOf(config.description).length,
        descriptionFirst: bytesOf(config.description)[0],
        descriptionDigest: digest(bytesOf(config.description)),
      });
      decoder.state = "configured";
    };

    decoder.decode = function (chunk) {
      if (decoder.state !== "configured") { throw new Error("the fake decoder is not configured"); }
      C.fed.push({
        decoder: id, type: chunk.type, timestamp: chunk.timestamp,
        bytes: chunk.data.length, digest: digest(chunk.data),
      });
      if (C.failAfter >= 0 && C.fed.length > C.failAfter) {
        var era = generation;
        queueMicrotask(function () {
          if (era !== generation || decoder.state === "closed") { return; }
          handlers.error({ message: "the fake decoder gave up part way through" });
        });
        return;
      }
      var previous = pending;
      pending = describe(chunk.data);
      if (previous) { deliver(previous, generation); }
    };

    decoder.flush = function () {
      C.flushes++;
      var era = generation;
      return new Promise(function (resolve) {
        queueMicrotask(function () {
          // The last output is delivered before the flush resolves, which is
          // the order the specification promises and the order the route
          // counts on: a resolution seen first would look like an end of
          // stream with a packet still owed.
          if (era === generation && decoder.state !== "closed" && pending) {
            handlers.output(audioData(pending, rate));
            pending = null;
          }
          resolve();
        });
      });
    };

    decoder.reset = function () { C.resets++; generation++; pending = null; decoder.state = "unconfigured"; };
    decoder.close = function () { C.closes++; generation++; pending = null; decoder.state = "closed"; };
  };

  globalThis.AudioDecoder.isConfigSupported = function (config) {
    C.probes++;
    C.probedWith = {
      codec: config.codec, sampleRate: config.sampleRate,
      numberOfChannels: config.numberOfChannels,
      descriptionBytes: bytesOf(config.description).length,
    };
    return Promise.resolve({ supported: C.supported, config: config });
  };
})();
`

// fakeCodecs installs the fake AudioDecoder for the length of the test. It is
// installed before the Engine starts, because the probe runs at init.
func fakeCodecs(t *testing.T, supported bool) *codecsFake {
	t.Helper()
	js.Global().Call("eval", fakeCodecsSource)
	js.Global().Get("__cogCodecs").Set("supported", supported)
	t.Cleanup(func() {
		js.Global().Delete("AudioDecoder")
		js.Global().Delete("EncodedAudioChunk")
		js.Global().Delete("__cogCodecs")
	})
	return &codecsFake{t: t, value: js.Global().Get("__cogCodecs")}
}

type codecsFake struct {
	t     *testing.T
	value js.Value
}

// failAfter makes the decoder report an error once it has been fed this many
// packets, which is the only way a decode fails after a Clip has installed.
func (c *codecsFake) failAfter(packets int) {
	c.value.Set("failAfter", packets)
}

func (c *codecsFake) count(field string) int { return c.value.Get(field).Int() }

// configured is every configure the route made, in order. A seek restarts the
// decoder, so a run that looped once configured twice.
func (c *codecsFake) configured() []js.Value { return each(c.value.Get("configured")) }

// fed is every EncodedAudioChunk the route handed over, in order.
func (c *codecsFake) fed() []js.Value { return each(c.value.Get("fed")) }

// syntheticVorbis builds an Ogg Vorbis bitstream whose audio packets say what
// they decode to, so that the fake decoder above can decode them and a test can
// assert about integers.
//
// It is a real container rather than a stub: real page headers, real segment
// tables, packets that run over a page boundary and have to be rejoined, and
// three setup headers whose lengths are what the decoder description's lacing is
// computed from. What is synthetic is only the audio, which is the part the
// browser would decode and the part a test cannot check by eye anyway.
func syntheticVorbis(channels, rate, packets, framesPerPacket int) []byte {
	identification := make([]byte, 30)
	copy(identification, "\x01vorbis")
	identification[11] = byte(channels)
	binary.LittleEndian.PutUint32(identification[12:16], uint32(rate))

	// Long enough that its length needs more than one lacing byte, which is the
	// branch in xiphExtradata that a short header would never reach.
	comment := make([]byte, 300)
	copy(comment, "\x03vorbis")
	setup := make([]byte, 600)
	copy(setup, "\x05vorbis")

	all := [][]byte{identification, comment, setup}
	granules := []int64{0, 0, 0}
	for index := range packets {
		// Padded past 255 bytes so every audio packet takes two segments, which
		// is what makes a packet land across a page boundary below.
		packet := make([]byte, 300)
		binary.LittleEndian.PutUint32(packet[0:4], uint32(index*framesPerPacket))
		binary.LittleEndian.PutUint32(packet[4:8], uint32(framesPerPacket))
		all = append(all, packet)
		granules = append(granules, int64((index+1)*framesPerPacket))
	}
	return oggPages(all, granules)
}

// oggPages writes packets into Ogg pages, five segments at a time, so that a
// packet of two segments regularly straddles a page boundary.
func oggPages(packets [][]byte, granules []int64) []byte {
	const perPage = 5
	var out []byte
	var lacing []byte
	var body []byte
	var granule int64
	continued := false
	sequence := uint32(0)

	flush := func(last bool) {
		if len(lacing) == 0 {
			return
		}
		page := make([]byte, oggHeaderBytes)
		copy(page, "OggS")
		flags := byte(0)
		if continued {
			flags |= 0x01
		}
		if sequence == 0 {
			flags |= 0x02
		}
		if last {
			flags |= 0x04
		}
		page[5] = flags
		binary.LittleEndian.PutUint64(page[6:14], uint64(granule))
		binary.LittleEndian.PutUint32(page[14:18], 0x0c06d15e)
		binary.LittleEndian.PutUint32(page[18:22], sequence)
		page[oggSegmentCount] = byte(len(lacing))
		out = append(out, page...)
		out = append(out, lacing...)
		out = append(out, body...)
		sequence++
		lacing, body, continued = nil, nil, false
	}

	for index, packet := range packets {
		segments := make([]byte, 0, len(packet)/255+1)
		length := len(packet)
		for length >= 255 {
			segments = append(segments, 255)
			length -= 255
		}
		segments = append(segments, byte(length))

		at := 0
		for taken := 0; taken < len(segments); {
			room := perPage - len(lacing)
			if room == 0 {
				// The page is full. Where the packet has already put segments
				// on it, the rest of it continues on the next page, which is
				// the case the walker has to rejoin.
				flush(false)
				continued = taken > 0
				room = perPage
			}
			fit := min(room, len(segments)-taken)
			run := 0
			for _, length := range segments[taken : taken+fit] {
				run += int(length)
			}
			lacing = append(lacing, segments[taken:taken+fit]...)
			body = append(body, packet[at:at+run]...)
			at, taken = at+run, taken+fit
			if taken == len(segments) {
				granule = granules[index]
			}
		}
	}
	flush(true)
	return out
}
