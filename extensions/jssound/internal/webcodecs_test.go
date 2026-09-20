//go:build js

package internal

import (
	"encoding/binary"
	"syscall/js"
	"testing"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// This suite is the WebCodecs route, and what it can and cannot say is worth
// stating before the assertions start.
//
// It cannot say that Chrome decodes what this hands it. node has no AudioDecoder
// and there is no browser here, so the decoder is a fake. What it can say - and
// what was missing when this route was first declined as untestable - is that
// every input the route produces is the input a real decoder was measured to
// accept: the packets that come out of the container, their order, and the byte
// layout of the description that carries the three setup headers. Those bytes
// are asserted against the fixture Clip below, and the same bytes for the same
// file were handed to a real Chrome AudioDecoder.isConfigSupported, which
// answered supported and then decoded the Clip to its exact granule length.
//
// So the fake is not standing in for the measurement. The measurement was made;
// the fake is what keeps the code that produced those bytes from drifting away
// from them.

// The fixture Clip's own demux, written down rather than computed, exactly as
// its frame count and rate are written down in backend_test.go - and for a
// stronger reason: these five numbers are what a real Chrome accepted. A change
// that moved any of them is a change to bytes a browser has agreed to, and it
// fails here rather than in a browser nobody is watching.
const (
	fixturePackets      = 52
	fixtureIDBytes      = 30
	fixtureCommentBytes = 281
	fixtureSetupBytes   = 4140
	fixtureDescription  = 4455
)

// The decoder description is the xiph extradata layout, byte for byte, and the
// fixture's is the one Chrome took.
func TestTheDecoderDescriptionIsTheLayoutARealDecoderAccepted(t *testing.T) {
	data := fixture(t).Data()

	packets, err := oggPackets(data)
	if err != nil {
		t.Fatalf("walking the fixture's pages: %v", err)
	}
	if len(packets) != fixturePackets {
		t.Fatalf("the fixture came apart into %d packets, want %d", len(packets), fixturePackets)
	}
	for index, want := range []struct {
		kind  byte
		bytes int
	}{{1, fixtureIDBytes}, {3, fixtureCommentBytes}, {5, fixtureSetupBytes}} {
		header := packets[index]
		if header[0] != want.kind || string(header[1:7]) != "vorbis" {
			t.Fatalf("header %d is type 0x%02x %q, want 0x%02x \"vorbis\"", index, header[0], header[1:7], want.kind)
		}
		if len(header) != want.bytes {
			t.Fatalf("header %d is %d bytes, want %d", index, len(header), want.bytes)
		}
	}

	stream, err := demuxVorbis(data)
	if err != nil {
		t.Fatalf("demuxing the fixture: %v", err)
	}
	if stream.channels != clipChannels || stream.sampleRate != clipRate {
		t.Fatalf("the identification header reads %d channels at %d Hz, want %d at %d",
			stream.channels, stream.sampleRate, clipChannels, clipRate)
	}
	if len(stream.packets) != fixturePackets-vorbisHeaders {
		t.Fatalf("the fixture has %d audio packets, want %d", len(stream.packets), fixturePackets-vorbisHeaders)
	}

	description := stream.description
	if len(description) != fixtureDescription {
		t.Fatalf("the description is %d bytes, want the %d a real decoder took", len(description), fixtureDescription)
	}
	// Two headers laced, then 30 in one byte, then 281 as 255 and 26 - the
	// branch a header shorter than 255 never reaches.
	if want := []byte{2, 30, 255, 26}; string(description[:4]) != string(want) {
		t.Fatalf("the description begins %v, want %v", description[:4], want)
	}
	at := 4
	for index, header := range [][]byte{packets[0], packets[1], packets[2]} {
		if string(description[at:at+len(header)]) != string(header) {
			t.Fatalf("header %d is not at offset %d of the description", index, at)
		}
		at += len(header)
	}
	if at != len(description) {
		t.Fatalf("the description holds %d bytes past its three headers, want none", len(description)-at)
	}
}

// A packet that runs off the end of a page is rejoined rather than cut. It is
// the one case in the container that is not a sub-slice, and a Vorbis setup
// header is long enough to hit it in almost every real file.
func TestAPacketThatSpansTwoPagesIsRejoined(t *testing.T) {
	const (
		packets = 24
		frames  = 1024
	)
	stream, err := demuxVorbis(syntheticVorbis(2, streamRate, packets, frames))
	if err != nil {
		t.Fatalf("demuxing a synthetic bitstream: %v", err)
	}
	if len(stream.packets) != packets {
		t.Fatalf("%d audio packets came out, want %d", len(stream.packets), packets)
	}
	for index, packet := range stream.packets {
		if len(packet) != 300 {
			t.Fatalf("audio packet %d is %d bytes, want the 300 it was written as - it was cut at a page", index, len(packet))
		}
		start := binary.LittleEndian.Uint32(packet[0:4])
		held := binary.LittleEndian.Uint32(packet[4:8])
		if int(start) != index*frames || int(held) != frames {
			t.Fatalf("audio packet %d says it starts at %d and holds %d, want %d and %d",
				index, start, held, index*frames, frames)
		}
	}
}

// The gate. A browser whose AudioDecoder refuses the Vorbis configuration keeps
// the Go decoder, and one that takes it gets WebCodecs - and the probe is asked
// once, with the description the streamed route will use rather than a codec
// string on its own.
func TestTheProbeDecidesTheStreamedRouteAndTheGoDecoderStaysTheFallback(t *testing.T) {
	for _, one := range []struct {
		name      string
		supported bool
		want      codecsSupport
	}{
		{name: "a browser that takes the config", supported: true, want: codecsPresent},
		{name: "a browser that refuses it", supported: false, want: codecsAbsent},
	} {
		t.Run(one.name, func(t *testing.T) {
			f := fakeAudio(t)
			c := fakeCodecs(t, one.supported)
			b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})

			if b.codecs != one.want {
				t.Fatalf("the probe settled at %v, want %v", b.codecs, one.want)
			}
			if probes := c.count("probes"); probes != 1 {
				t.Fatalf("the Adapter asked isConfigSupported %d times, want exactly one", probes)
			}
			asked := c.value.Get("probedWith")
			if codec := asked.Get("codec").String(); codec != "vorbis" {
				t.Fatalf("the probe asked about %q, want \"vorbis\"", codec)
			}
			if bytes := asked.Get("descriptionBytes").Int(); bytes != fixtureDescription {
				t.Fatalf("the probe carried a %d byte description, want the micro-clip's %d", bytes, fixtureDescription)
			}

			source, err := b.opener()(fixture(t))
			if err != nil {
				t.Fatalf("opening a decoder over the fixture: %v", err)
			}
			defer source.close()
			_, webCodecs := source.(*codecsSource)
			if webCodecs != one.supported {
				t.Fatalf("the streamed route opened %T, want WebCodecs=%v", source, one.supported)
			}
		})
	}
}

// A browser with no WebCodecs at all is the node this suite runs on, and it must
// cost nothing: no probe, no wait, and the Go decoder as it always was.
func TestAPageWithNoAudioDecoderSettlesAtOnceAndKeepsTheGoDecoder(t *testing.T) {
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})

	if b.codecs != codecsAbsent {
		t.Fatalf("a page with no AudioDecoder settled at %v, want codecsAbsent", b.codecs)
	}
	source, err := b.opener()(fixture(t))
	if err != nil {
		t.Fatalf("opening a decoder over the fixture: %v", err)
	}
	defer source.close()
	if _, webCodecs := source.(*codecsSource); webCodecs {
		t.Fatal("a page with no AudioDecoder opened a WebCodecs decoder")
	}
}

// A streamed Clip decodes through the browser's own decoder, and the frames that
// come back are the frames that went in.
//
// This is the whole route end to end: the container walked in Go, the packets
// fed as EncodedAudioChunks in order, the AudioData read back, the chunks
// scheduled on the context clock. The fake decodes each packet into the frame
// numbers the packet says it holds, so what the render asserts is integers.
func TestAStreamedClipDecodesThroughWebCodecsAndKeepsItsFrames(t *testing.T) {
	const (
		packets   = 48
		perPacket = 1024
		rendered  = 24000
	)
	f := fakeAudio(t)
	c := fakeCodecs(t, true)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})

	clip := playCodecs(t, f, b, c, packets, perPacket, m.Maybe[sound.LoopRegion]{}, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 24)

	if !clip.streams() {
		t.Fatal("the Clip did not stream")
	}
	out := f.renderOffline(float64(rendered) / streamRate)
	for k := range int64(rendered) {
		if out[0][k] != float32(k) || out[1][k] != -float32(k) {
			t.Fatalf("output frame %d carries source frame %v/%v, want %v - WebCodecs frames are in the wrong place",
				k, out[0][k], -out[1][k], k)
		}
	}

	// The decoder was configured as Vorbis at the Clip's own rate, with the
	// description the demuxer built.
	configured := c.configured()
	if len(configured) == 0 {
		t.Fatal("no AudioDecoder was configured")
	}
	first := configured[0]
	if codec := first.Get("codec").String(); codec != "vorbis" {
		t.Fatalf("the decoder was configured for %q, want \"vorbis\"", codec)
	}
	if rate := first.Get("sampleRate").Int(); rate != streamRate {
		t.Fatalf("the decoder was configured at %d Hz, want the file's %d", rate, streamRate)
	}
	if channels := first.Get("numberOfChannels").Int(); channels != 2 {
		t.Fatalf("the decoder was configured for %d channels, want 2", channels)
	}

	// The packets reached it in order, unmodified, and no setup header was ever
	// fed as audio - which a digest over the bytes says and a count of them
	// could not.
	stream, err := demuxVorbis(clip.encoded.Data())
	if err != nil {
		t.Fatalf("demuxing what was played: %v", err)
	}
	fed := c.fed()
	if len(fed) == 0 {
		t.Fatal("no packet reached the decoder")
	}
	if len(fed) > len(stream.packets) {
		t.Fatalf("%d packets were fed from a Clip that has %d", len(fed), len(stream.packets))
	}
	var lastTimestamp int64 = -1
	for index, chunk := range fed {
		if kind := chunk.Get("type").String(); kind != "key" {
			t.Fatalf("packet %d was fed as %q, want \"key\"", index, kind)
		}
		if got, want := chunk.Get("digest").Int(), digestOf(stream.packets[index]); got != want {
			t.Fatalf("packet %d is not the Clip's packet %d", index, index)
		}
		at := int64(chunk.Get("timestamp").Float())
		if at <= lastTimestamp {
			t.Fatalf("packet %d carries timestamp %d, which does not follow %d", index, at, lastTimestamp)
		}
		lastTimestamp = at
	}
}

// Every AudioData is closed, and the decoder is given back with the Voice that
// opened it.
//
// Both are leaks that a garbage collection does not clear up. An AudioData holds
// decoded audio outside the JS heap's own accounting, and the two js.Funcs the
// decoder answers through are the one thing syscall/js will not collect - so a
// Voice that forgot either would cost the page a little more every time
// something played.
func TestAReleaseClosesEveryAudioDataAndGivesTheDecoderBack(t *testing.T) {
	const packets = 64
	f := fakeAudio(t)
	c := fakeCodecs(t, true)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})

	playCodecs(t, f, b, c, packets, 1024, m.Maybe[sound.LoopRegion]{}, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 12)
	live := b.slots[0].live
	id := sound.ClipID(b.lastID)

	f.advance(0.1)
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})
	<-live.stream.done
	f.yield()

	if made, closed := c.count("dataMade"), c.count("dataClosed"); made == 0 || made != closed {
		t.Fatalf("%d AudioData were made and %d closed, want every one closed and some made", made, closed)
	}
	if made, closed := c.count("decoders"), c.count("closes"); made != closed {
		t.Fatalf("%d AudioDecoders were made and %d closed", made, closed)
	}
}

// The Loop Region works on this route too, and it is the hard half of it:
// WebCodecs has no seek, so the wrap restarts the decoder and throws away
// everything before the loop point.
//
// What the promise needs is that the discard is exact. The loop start here is
// deliberately not a packet boundary, so the wrap lands in the middle of an
// AudioData and the frames either side of the join are asserted one by one -
// none repeated, none dropped, and no silence where the wrap is.
func TestALoopingStreamedVoiceIsGaplessOnTheWebCodecsRoute(t *testing.T) {
	const (
		packets   = 40
		perPacket = 1024
		loopStart = 8000
		loopEnd   = 19200
		rendered  = 24000
	)
	f := fakeAudio(t)
	c := fakeCodecs(t, true)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})

	playCodecs(t, f, b, c, packets, perPacket, m.Some(sound.LoopRegion{
		Start: float32(loopStart) / streamRate,
		End:   float32(loopEnd) / streamRate,
	}), sound.VoiceStart{
		Slot: 0, Loop: true, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 28)

	out := f.renderOffline(float64(rendered) / streamRate)
	for k := range int64(rendered) {
		want := float32(expectedFrame(k, 0, loopStart, loopEnd))
		if out[0][k] != want || out[1][k] != -want {
			t.Fatalf("output frame %d carries source frame %v/%v, want %v - the loop is not gapless on WebCodecs",
				k, out[0][k], -out[1][k], want)
		}
	}
	if resets := c.count("resets"); resets == 0 {
		t.Fatal("the loop wrapped without restarting the decoder, which WebCodecs has no other way to do")
	}
	if configured := len(c.configured()); configured < 2 {
		t.Fatalf("the decoder was configured %d times, want one per restart", configured)
	}
}

// A decoder that fails part way through ends the Voice, exactly as the Go route
// does. There is nothing to report: a Clip that parsed, installed and has been
// playing has already been told everything it is owed, and what is left is
// silence and an ending on sound's own playhead.
func TestADecoderThatFailsPartWayThroughEndsTheVoice(t *testing.T) {
	const packets = 64
	f := fakeAudio(t)
	c := fakeCodecs(t, true)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})
	c.failAfter(6)

	playCodecs(t, f, b, c, packets, 1024, m.Maybe[sound.LoopRegion]{}, sound.VoiceStart{
		Slot: 0, Loop: true, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 16)

	live := b.slots[0].live
	if !live.stream.finished() {
		t.Fatal("the decode-ahead is still producing after the decoder failed")
	}
	<-live.stream.done

	// A looping Voice whose decoder failed does not wrap: a wrap is a seek into
	// a decoder that has nothing left to give, and spinning on one would be a
	// Voice that never ends. What is asserted is that feeding stopped, not how
	// far it had got - the error arrives from the event loop, so packets already
	// in flight when it was raised are packets the route had every right to send.
	fed := len(c.fed())
	for range 6 {
		f.yield()
		b.Emit(&sound.Batch{})
		f.advance(1.0 / 60)
	}
	if after := len(c.fed()); after != fed {
		t.Fatalf("%d more packets were fed after the decoder failed, want none", after-fed)
	}
	if completed := b.TakePrepared(); len(completed) != 0 {
		t.Fatalf("a decoder failing mid-stream reported %v, want nothing - the Clip already installed", completed)
	}
	if made, closed := c.count("dataMade"), c.count("dataClosed"); made != closed {
		t.Fatalf("%d AudioData were made and %d closed after a failure", made, closed)
	}
}

// playCodecs prepares a synthetic Clip through the backend's own dispatch - so
// the route is the one the probe chose rather than one the test picked - installs
// it, starts a Voice and pumps the flush until the schedule covers the window a
// render will ask for.
func playCodecs(
	t *testing.T, f *audioFake, b *backend, c *codecsFake,
	packets, perPacket int, region m.Maybe[sound.LoopRegion],
	start sound.VoiceStart, ticks int,
) *clipData {
	t.Helper()
	encoded := assets.NewBlob(syntheticVorbis(2, streamRate, packets, perPacket))
	frames := int64(packets * perPacket)
	clip := &clipData{
		buffer:     js.Undefined(),
		duration:   float32(frames) / float32(streamRate),
		channels:   2,
		sourceRate: streamRate,
		frames:     frames,
		region:     region,
		encoded:    encoded,
	}

	b.dispatch(waiting{token: "codecs", encoded: encoded, clip: clip})
	f.yield()
	completed := b.TakePrepared()
	if len(completed) != 1 || completed[0].Err != nil {
		t.Fatalf("the synthetic Clip completed as %v", completed)
	}
	id, err := b.Install(completed[0].Clip)
	if err != nil {
		t.Fatalf("Install refused the synthetic Clip: %v", err)
	}

	start.Clip = id
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{start}})

	// The clock is held still until the first chunk is on it, and that is a
	// statement about this route rather than a convenience. A decoder that
	// answers from the event loop cannot produce a Voice's first chunk inside
	// the flush that started it: it takes a turn of the page, where the Go
	// decoder takes a turn of the Go scheduler. The Adapter already handles a
	// chunk that arrives after its own start time - it starts it at now with an
	// offset into itself - so letting the clock run here would assert start
	// latency, which stream_test.go's late-chunk test is about, instead of
	// where frames land, which this one is.
	for range 40 {
		if len(f.sources()) > 0 {
			break
		}
		f.yield()
		b.Emit(&sound.Batch{})
	}
	if len(f.sources()) == 0 {
		t.Fatal("no chunk was scheduled at all")
	}
	for range ticks {
		f.yield()
		b.Emit(&sound.Batch{})
		f.advance(1.0 / 60)
	}
	return clip
}

// digestOf is the fake decoder's own rolling sum over a packet's bytes, written
// again here so that "this is the Clip's packet" is checked against the bytes
// rather than against a length.
func digestOf(packet []byte) int {
	sum := uint32(0)
	for _, value := range packet {
		sum = sum*31 + uint32(value)
	}
	return int(sum)
}
