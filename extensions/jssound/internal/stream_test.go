//go:build js

package internal

import (
	"errors"
	"io"
	"math"
	"syscall/js"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// This suite is the streamed tier, and most of it runs on a Clip with no Ogg in
// it at all: a decoder whose every frame is its own frame number.
//
// That is the point. "No repeated or dropped sample frame at the loop point" is
// a claim about which frame is where, and against a real Clip it could only be
// checked by comparing floats to floats produced by the same code. Against a
// ramp it is an assertion about integers: output frame k must carry source frame
// number f(k), and a loop that repeated one or dropped one is an integer in the
// wrong place.
//
// The real fixture is still here, for the things a synthetic source cannot say:
// that the two tiers report one Clip identically, and that the real decoder and
// the real resampler produce a schedule with no holes in it.

// identity is the gain matrix that routes source channel 0 to output 0 and
// source channel 1 to output 1 and nothing else, so a render is the Clip's own
// samples rather than a mix of them.
var identity = [2][2]float32{{1, 0}, {0, 1}}

// streamRate is the rate the fake context runs at. A synthetic Clip is made at
// exactly this rate so that no resampler stands between a source frame and an
// output frame - the resampler has its own test below, and mixing the two would
// make a seam failure and a filter failure the same red.
const streamRate = 48000

// rampSource is a decoder whose frame n reads n in channel 0 and -n in channel
// 1. float32 holds every integer up to 2^24 exactly, so a rendered frame either
// is the frame that belongs there or is visibly not.
type rampSource struct {
	frames   int64
	channels int
	at       int64
	// opened, seeks and closes count what the tier asked of it, which is how a
	// resume that re-places chunks is told apart from one that restarts a
	// decoder.
	seeks  int
	closes int
}

func (r *rampSource) read(dst []float32) (int, error) {
	if r.at >= r.frames {
		return 0, io.EOF
	}
	frames := int64(len(dst) / r.channels)
	if room := r.frames - r.at; frames > room {
		frames = room
	}
	for f := range frames {
		for c := range r.channels {
			value := float32(r.at + f)
			if c == 1 {
				value = -value
			}
			dst[f*int64(r.channels)+int64(c)] = value
		}
	}
	r.at += frames
	return int(frames) * r.channels, nil
}

func (r *rampSource) seek(frame int64) error {
	r.seeks++
	r.at = frame
	return nil
}

// closed counts the closes, because the tier promises a decoder is given back
// with the Voice that opened it - which is a leak of two js.Funcs a play on the
// WebCodecs route where it is not.
func (r *rampSource) close() { r.closes++ }

// rampClip builds a streamed Clip over a ramp, at the context's own rate. It is
// a clipData made by hand rather than through Prepare because what is under test
// is the tier and not the header parse, and because no Ogg file says "frame n
// holds n".
func rampClip(frames int64, region m.Maybe[sound.LoopRegion]) (*clipData, *[]*rampSource) {
	opened := new([]*rampSource)
	return &clipData{
		buffer:     js.Undefined(),
		duration:   float32(frames) / float32(streamRate),
		channels:   2,
		sourceRate: streamRate,
		frames:     frames,
		region:     region,
		open: func(assets.Blob) (streamSource, error) {
			source := &rampSource{frames: frames, channels: 2}
			*opened = append(*opened, source)
			return source, nil
		},
	}, opened
}

// playRamp installs a ramp Clip, starts a Voice on it, and pumps the flush
// forward until the schedule covers the window a render will ask for.
//
// The clock moves in whole ticks between pumps, which is what a game does, and
// the yield before each pump is the event loop the decode-ahead runs on. A frame
// does both by ending; a test has to say so.
func playRamp(t *testing.T, f *audioFake, b *backend, clip *clipData, start sound.VoiceStart, ticks int) {
	t.Helper()
	if _, err := b.Install(clip); err != nil {
		t.Fatalf("Install refused a streamed Clip: %v", err)
	}
	start.Clip = sound.ClipID(b.lastID)
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{start}})
	for range ticks {
		f.yield()
		b.Emit(&sound.Batch{})
		f.advance(1.0 / 60)
	}
}

// expectedFrame is where output frame k of a run beginning at source frame from
// reads, wrapping inside the Loop Region. It is the arithmetic the promise is
// about, written once, independently of the code that has to satisfy it.
func expectedFrame(k, from, loopStart, loopEnd int64) int64 {
	at := from + k
	if at < loopEnd {
		return at
	}
	return loopStart + (at-loopEnd)%(loopEnd-loopStart)
}

// The load-bearing promise: a looping streamed Voice is gapless at its loop
// point. The graph the Adapter built is rendered offline and every frame in the
// window is the frame that belongs there - none repeated, none dropped, and no
// silence where the wrap is.
//
// The window spans two wraps, because one wrap can be got right by accident: a
// scheduler that repeated a fraction of a chunk would show it on the second.
func TestALoopingStreamedVoiceIsGaplessAtItsLoopPoint(t *testing.T) {
	const (
		frames    = 30000
		loopStart = 9600
		loopEnd   = 19200
		rendered  = 24000
	)
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, _ := rampClip(frames, m.Some(sound.LoopRegion{
		Start: float32(loopStart) / streamRate,
		End:   float32(loopEnd) / streamRate,
	}))

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Loop: true, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 16)

	out := f.renderOffline(float64(rendered) / streamRate)
	for k := range int64(rendered) {
		want := float32(expectedFrame(k, 0, loopStart, loopEnd))
		if out[0][k] != want || out[1][k] != -want {
			t.Fatalf("output frame %d carries source frame %v/%v, want %v - the loop is not gapless",
				k, out[0][k], -out[1][k], want)
		}
	}
}

// A one-shot streamed Voice runs out where its Clip does and nothing is
// scheduled past it. The Voice itself ends on sound's own playhead, exactly as a
// resident one whose buffer ran out does, which is what keeps the tier invisible.
func TestAStreamedOneShotStopsProducingAtTheEndOfItsClip(t *testing.T) {
	const frames = 12000
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, _ := rampClip(frames, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 8)

	out := f.renderOffline(0.5)
	for k := range int64(frames) {
		if out[0][k] != float32(k) {
			t.Fatalf("output frame %d carries %v, want %v", k, out[0][k], k)
		}
	}
	for k := int64(frames); k < 24000; k++ {
		if out[0][k] != 0 {
			t.Fatalf("output frame %d past the end of the Clip carries %v, want silence", k, out[0][k])
		}
	}
	live := b.slots[0].live
	if !live.stream.finished() {
		t.Fatal("the decode-ahead is still producing past the end of a one-shot's Clip")
	}
}

// Chunks are scheduled back to back on the context clock: each one's `when` is
// the one before it plus its own length in context frames, its offset is zero,
// and no `when` is ever in the past. A seam reached by a `when` that had already
// passed is Web Audio starting a source immediately, which is a doubled fragment
// rather than a late start.
func TestChunksAreScheduledBackToBackAndNeverInThePast(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, _ := rampClip(200000, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 12)

	sources := f.sources()
	if len(sources) < 12 {
		t.Fatalf("a streamed Voice scheduled %d chunks, want a schedule worth of them", len(sources))
	}
	var next float64
	for i, source := range sources {
		started := source.Get("started")
		if !started.Truthy() {
			t.Fatalf("chunk %d was made and never started", i)
		}
		when, offset := started.Get("when").Float(), started.Get("offset").Float()
		if offset != 0 {
			t.Fatalf("chunk %d started at offset %v, want its own head - an offset is a stall recovery", i, offset)
		}
		if i > 0 && math.Abs(when-next) > 1e-9 {
			t.Fatalf("chunk %d starts at %v, want %v - the chunks are not back to back", i, when, next)
		}
		next = when + source.Get("buffer").Get("length").Float()/streamRate
	}
	if late := b.slots[0].live.late; late != 0 {
		t.Fatalf("%d chunks were scheduled with a `when` already past, want none", late)
	}
}

// A main-thread stall long enough to swallow the read-ahead is survived rather
// than heard as a click: a chunk whose `when` has already passed is never handed
// to Web Audio to start immediately. It is started at now with an offset into
// itself, or dropped if the whole of it is behind - which is the Device's own
// rule one level down, a playhead advancing whether or not anyone could hear it.
func TestAStallIsRecoveredFromRatherThanScheduledInThePast(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, _ := rampClip(2000000, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 4)
	scheduled := len(f.sources())

	// Two seconds of wall clock with no flush at all: twice the read-ahead.
	f.advance(2)
	for range 40 {
		f.yield()
		b.Emit(&sound.Batch{})
	}

	live := b.slots[0].live
	if live.late == 0 {
		t.Fatal("a two-second stall produced no late chunk at all, so this asserts nothing")
	}
	now := f.context().Get("currentTime").Float()
	for _, source := range f.sources()[scheduled:] {
		if when := source.Get("started").Get("when").Float(); when < now-1e-9 {
			t.Fatalf("a chunk was scheduled at %v, before the clock's %v", when, now)
		}
	}
	if len(f.sources()) <= scheduled {
		t.Fatal("the Voice never caught up: nothing was scheduled after the stall")
	}
}

// A Seek is a VoiceStart with an Offset on a live slot, and on a streamed Voice
// it re-schedules from the new offset rather than moving a cursor: the old
// chunks are stopped, a fresh decoder is opened, and it is seeked to the frame
// the offset names.
func TestASeekOnAStreamedVoiceReschedulesFromTheNewOffset(t *testing.T) {
	const frames = 200000
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, opened := rampClip(frames, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 6)
	before := f.sources()

	f.advance(0.1)
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: sound.ClipID(b.lastID), Offset: time.Second,
		Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}}})
	for range 4 {
		f.yield()
		b.Emit(&sound.Batch{})
	}

	for i, source := range before {
		if !source.Get("stopped").Truthy() {
			t.Fatalf("chunk %d of the Voice the Seek replaced was left playing", i)
		}
	}
	if len(*opened) != 2 {
		t.Fatalf("the Seek opened %d decoders in total, want 2 - it cannot rewind the first", len(*opened))
	}
	if (*opened)[1].seeks != 1 {
		t.Fatalf("the Seek's decoder was seeked %d times, want once to the offset", (*opened)[1].seeks)
	}
	// One second in at 48 kHz is source frame 48000, and the ramp says so.
	fresh := f.sources()[len(before):]
	if len(fresh) == 0 {
		t.Fatal("the Seek scheduled no chunk of its own")
	}
	first := fresh[0].Get("buffer").Get("filled").Index(0).Get("first").Float()
	if first != streamRate {
		t.Fatalf("the Seek's first chunk begins at source frame %v, want %d", first, streamRate)
	}
}

// A release stops a streamed Voice, every chunk it had scheduled, and its
// decode-ahead. The last of those is the spec's own line - "a streamed Voice's
// read-ahead stops with it" - and it holds however the Voice ended, because
// finished, stolen, released and cut all reach this Adapter as the same stop.
func TestAReleaseStopsAStreamedVoiceItsChunksAndItsReadAhead(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, _ := rampClip(2000000, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 4)
	live := b.slots[0].live
	id := sound.ClipID(b.lastID)

	f.advance(0.1)
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})

	for i, source := range f.sources() {
		if !source.Get("stopped").Truthy() {
			t.Fatalf("chunk %d was still playing after the release", i)
		}
	}
	if !live.stream.halted() {
		t.Fatal("the decode-ahead was not halted by the release")
	}
	<-live.stream.done
	if _, held := b.clips[id]; held {
		t.Fatal("the destroy left the Clip in the table")
	}
}

// Pausing a streamed Voice stops its chunks and remembers where it was;
// resuming puts the same buffers back on the clock rather than seeking a
// decoder, so a resume is sample-continuous and costs nothing.
//
// This is the second derivation #488 handed over, answered by not needing one.
// The chunks sit on the context clock, so the context clock is the playhead:
// position() derives it from (offset, contextTime, rate) exactly as it does for
// a resident Voice, and both tiers agree about where a paused ambience resumes.
func TestPausingAStreamedVoiceResumesTheSameSamplesWithNoDecoderSeek(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, opened := rampClip(2000000, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 6)
	live := b.slots[0].live

	f.advance(0.25)
	at := f.context().Get("currentTime").Float()
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1, Paused: true},
	}}})
	paused := live.position(at)
	if paused != at {
		t.Fatalf("the pause left the playhead at %v, want the %v seconds the Voice had played", paused, at)
	}

	// Ten seconds of pause. The playhead does not move, and neither does the
	// decoder: what was decoded is still the right samples.
	f.advance(10)
	f.yield()
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}}})
	resumed := f.context().Get("currentTime").Float()
	if at := live.position(resumed); at != paused {
		t.Fatalf("the resume landed at %v, want the %v the pause left", at, paused)
	}
	if len(*opened) != 1 {
		t.Fatalf("a pause and a resume opened %d decoders, want the one the Voice started with", len(*opened))
	}
	if seeks := (*opened)[0].seeks; seeks != 0 {
		t.Fatalf("a resume seeked the decoder %d times, want none - it re-places chunks", seeks)
	}
}

// A rate change re-places the schedule rather than setting playbackRate and
// hoping. Every `when` already on the clock was computed at the old rate, so the
// chunks after the one in flight would play at the new speed from the old times
// and every seam after it would open; the buffers go back on the clock from
// their frame numbers instead, and the decoder never moves.
func TestARateChangeRePlacesTheScheduleAndKeepsTheDecoderWhereItWas(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	clip, opened := rampClip(2000000, m.Maybe[sound.LoopRegion]{})

	playRamp(t, f, b, clip, sound.VoiceStart{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}, 6)
	live := b.slots[0].live
	before := len(f.sources())

	f.advance(0.1)
	now := f.context().Get("currentTime").Float()
	at := live.position(now)
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: identity, Rate: 2},
	}}})

	if position := live.position(now); position != at {
		t.Fatalf("the rate change moved the playhead to %v from %v", position, at)
	}
	fresh := f.sources()[before:]
	if len(fresh) == 0 {
		t.Fatal("the rate change re-placed nothing, so the old schedule is still on the clock at the old rate")
	}
	for i, source := range fresh {
		if rate := source.Get("playbackRate").Get("value").Float(); rate != 2 {
			t.Fatalf("re-placed chunk %d plays at %v, want the new rate", i, rate)
		}
		if when := source.Get("started").Get("when").Float(); when < now-1e-9 {
			t.Fatalf("re-placed chunk %d was put at %v, before the clock's %v", i, when, now)
		}
	}
	if len(*opened) != 1 {
		t.Fatalf("a rate change opened %d decoders, want none beyond the Voice's own", len(*opened))
	}
	if seeks := (*opened)[0].seeks; seeks != 0 {
		t.Fatalf("a rate change seeked the decoder %d times, want none", seeks)
	}
}

// No HTMLMediaElement, and the count says so rather than a comment.
//
// It was checked and refused for a Clip: Chromium and WebKit implement an
// element's loop as a seek, so a loop through one is not gapless and the promise
// above could not be kept; WebKit's gesture gate is per element, so every track
// would need a gesture of its own rather than the one the context already has;
// and the element runs on its own clock, so a chunk could not be landed on a
// context time at all.
func TestNoMediaElementIsEverMadeForAClip(t *testing.T) {
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})
	id, _ := streamed(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Loop: true, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}}})
	for range 6 {
		f.yield()
		b.Emit(&sound.Batch{})
		f.advance(1.0 / 60)
	}

	if made := f.mediaElements(); made != 0 {
		t.Fatalf("the Adapter made %d media elements or media element sources, want none", made)
	}
	if f.panners() != 0 {
		t.Fatalf("a streamed Voice made %d panner nodes, want none", f.panners())
	}
}

// streamed prepares and installs a Clip that will stream, driving the completion
// the way sound's flush does. Nothing is decoded, so the only thing to wait for
// is the queue.
func streamed(t *testing.T, f *audioFake, b *backend, encoded assets.Blob) (sound.ClipID, sound.PreparedClip) {
	t.Helper()
	prepared, done, err := b.Prepare("streamed", encoded)
	if err != nil {
		t.Fatalf("Prepare refused a Clip that should stream: %v", err)
	}
	if done || prepared != nil {
		t.Fatal("Prepare answered done, and every route through this Adapter is a callback")
	}
	f.yield()
	completed := b.TakePrepared()
	if len(completed) != 1 {
		t.Fatalf("TakePrepared drained %d prepares, want 1", len(completed))
	}
	if completed[0].Err != nil {
		t.Fatalf("the streamed prepare failed: %v", completed[0].Err)
	}
	id, err := b.Install(completed[0].Clip)
	if err != nil {
		t.Fatalf("Install refused a streamed Clip: %v", err)
	}
	return id, completed[0].Clip
}

// The tier is invisible to the game: the same bytes prepared either side of the
// limit report the same duration, the same channels, the same rate and the same
// loop region. That is the command surface's rule holding rather than being
// reopened, and it is why Config.DecodedClipLimit can live in the Adapter at all.
func TestBothTiersReportOneClipIdentically(t *testing.T) {
	encoded := fixture(t)

	whole := func() sound.PreparedClip {
		f := fakeAudio(t)
		b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: neverStream})
		_, clip := resident(t, f, b, encoded)
		return clip
	}()
	streaming := func() sound.PreparedClip {
		f := fakeAudio(t)
		b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})
		_, clip := streamed(t, f, b, encoded)
		return clip
	}()

	if whole.Duration() != streaming.Duration() {
		t.Fatalf("the tiers report %v and %v seconds for one Clip", whole.Duration(), streaming.Duration())
	}
	if whole.Channels() != streaming.Channels() {
		t.Fatalf("the tiers report %d and %d channels", whole.Channels(), streaming.Channels())
	}
	if whole.SampleRate() != streaming.SampleRate() {
		t.Fatalf("the tiers report %d Hz and %d Hz", whole.SampleRate(), streaming.SampleRate())
	}
	one, oneOK := whole.LoopRegion().Get()
	two, twoOK := streaming.LoopRegion().Get()
	if oneOK != twoOK || one != two {
		t.Fatalf("the tiers report loop regions %v/%v and %v/%v", one, oneOK, two, twoOK)
	}
	if whole.Duration() != clipDuration {
		t.Fatalf("both tiers report %v seconds, and the file is %v", whole.Duration(), clipDuration)
	}
}

// DecodedClipLimit draws the line, on decoded size computed before decoding,
// with otosound's sentinels and otosound's default. The table is the contract
// the two Configs share.
func TestDecodedClipLimitDrawsTheLineWhereItSays(t *testing.T) {
	const under = int64(defaultDecodedClipLimit)
	for _, one := range []struct {
		limit   int
		decoded int64
		streams bool
	}{
		{limit: 0, decoded: under, streams: false},
		{limit: 0, decoded: under + 1, streams: true},
		{limit: neverStream, decoded: 1 << 30, streams: false},
		{limit: alwaysStream, decoded: 1, streams: true},
		{limit: 1024, decoded: 1024, streams: false},
		{limit: 1024, decoded: 1025, streams: true},
	} {
		if got := overLimit(one.limit, one.decoded); got != one.streams {
			t.Fatalf("a %d byte Clip under a limit of %d streams=%v, want %v",
				one.decoded, one.limit, got, one.streams)
		}
	}

	// And the real Clip lands on the side the arithmetic says. The fixture is
	// 48704 frames of stereo, which is 389632 bytes decoded - under the default.
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{})
	_, clip := resident(t, f, b, fixture(t))
	if clip.(*clipData).streams() {
		t.Fatalf("a %d byte Clip streamed under the %d byte default",
			clip.(*clipData).decodedBytes(), defaultDecodedClipLimit)
	}

	g := fakeAudio(t)
	c := startedWith(t, g, true, jssound.Config{DecodedClipLimit: 1 << 10})
	_, long := streamed(t, g, c, fixture(t))
	if !long.(*clipData).streams() {
		t.Fatalf("a %d byte Clip did not stream under a 1 KiB limit", long.(*clipData).decodedBytes())
	}
}

// A streamed Clip decodes nothing at load. That is the memory claim the whole
// tier exists for: the 106 MB a five-minute track would cost resident is never
// allocated, and the browser is never asked to decode anything.
func TestAStreamedClipDecodesNothingAtLoad(t *testing.T) {
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})
	buffers := len(f.buffers())

	streamed(t, f, b, fixture(t))

	if made := len(f.buffers()) - buffers; made != 0 {
		t.Fatalf("a streamed prepare made %d AudioBuffers, want none until a Voice asks", made)
	}
	if asked := f.value.Get("pending").Length(); asked != 0 {
		t.Fatalf("a streamed prepare asked the browser for %d decodes, want none", asked)
	}
}

// A Clip whose granule positions name no length streams rather than failing.
// ErrNoStreamLength used to be terminal here; the spec says a Clip with no
// computable decoded size streams, and otosound has always done that, so what is
// left of the error is a stream that really does hold no frames.
func TestAClipWithNoGranuleLengthStreamsAndIsCountedRatherThanRefused(t *testing.T) {
	const frames = 7000
	clip, _ := rampClip(frames, m.Maybe[sound.LoopRegion]{})
	clip.frames, clip.duration, clip.unmeasured = 0, 0, true

	if err := clip.measure(); err != nil {
		t.Fatalf("measuring a Clip with no granule length: %v", err)
	}
	if clip.frames != frames {
		t.Fatalf("the measured Clip holds %d frames, want %d", clip.frames, frames)
	}
	if clip.duration != float32(frames)/streamRate {
		t.Fatalf("the measured Clip is %v seconds, want %v", clip.duration, float32(frames)/streamRate)
	}
	if clip.unmeasured {
		t.Fatal("the Clip is still unmeasured after being measured")
	}

	// A stream that truly holds no frames is still the terminal failure it was.
	empty, _ := rampClip(0, m.Maybe[sound.LoopRegion]{})
	empty.frames, empty.unmeasured = 0, true
	var want jssound.ErrNoStreamLength
	if err := empty.measure(); err != want {
		t.Fatalf("measuring an empty stream gave %v, want %v", err, want)
	}
}

// The tier decision is dispatch's, and an unmeasured Clip reaches the streamed
// route whatever the limit says - including under -1, which refuses to stream
// anything that has a size to compare.
func TestAnUnmeasuredClipStreamsEvenWhereNothingElseWould(t *testing.T) {
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: neverStream})
	clip, _ := rampClip(5000, m.Maybe[sound.LoopRegion]{})
	clip.frames, clip.duration, clip.unmeasured = 0, 0, true

	b.dispatch(waiting{token: "unmeasured", encoded: fixture(t), clip: clip})
	f.yield()

	completed := b.TakePrepared()
	if len(completed) != 1 || completed[0].Err != nil {
		t.Fatalf("the unmeasured Clip completed as %v", completed)
	}
	if !clip.streams() {
		t.Fatal("the unmeasured Clip did not stream")
	}
	if asked := f.value.Get("pending").Length(); asked != 0 {
		t.Fatalf("the unmeasured Clip asked the browser for %d decodes, want none", asked)
	}
	if clip.frames != 5000 {
		t.Fatalf("the unmeasured Clip was counted at %d frames, want 5000", clip.frames)
	}
}

// Register refuses a DecodedClipLimit that names no tier, exactly as otosound's
// does, because a limit below -2 is neither a size nor a sentinel.
func TestRegisterRefusesADecodedClipLimitBelowTheSentinels(t *testing.T) {
	fakeAudio(t)
	var refused jssound.ErrInvalidDecodedClipLimit
	err := (&plugin{}).Register(&kernel.Registrar{}, jssound.Config{DecodedClipLimit: -3})
	if !errors.As(err, &refused) {
		t.Fatalf("Register accepted a DecodedClipLimit of -3: err = %v", err)
	}
	if refused.DecodedClipLimit != -3 {
		t.Fatalf("Register refused carrying %d, want -3", refused.DecodedClipLimit)
	}
}

// The resampler is what makes a chunk boundary free, and it is free only because
// one conversion spans every chunk: a filter restarted at a seam would put its
// own edge there. Converting in pieces must be sample-for-sample what converting
// the whole was.
func TestConvertingInChunksIsWhatConvertingWholeWouldHaveBeen(t *testing.T) {
	const (
		from   = 44100
		to     = 48000
		frames = 20000
	)
	input := make([]float32, frames*2)
	for f := range frames {
		// Two decorrelated tones, so a channel swapped or a window restarted
		// shows up rather than cancelling.
		input[f*2] = float32(0.5 * math.Sin(2*math.Pi*0.031*float64(f)))
		input[f*2+1] = float32(0.4 * math.Sin(2*math.Pi*0.113*float64(f)))
	}
	filter := newSincFilter(from, to)

	whole := newResampler(filter, 2)
	whole.feed(input)
	one := make([]float32, (frames*2+64)*2)
	wholeFrames := whole.drain(one, true)

	piecewise := newResampler(filter, 2)
	two := make([]float32, (frames*2+64)*2)
	at, piece := 0, 512*2
	for cut := 0; cut < len(input); cut += piece {
		end := min(cut+piece, len(input))
		piecewise.feed(input[cut:end])
		at += piecewise.drain(two[at*2:], false)
	}
	at += piecewise.drain(two[at*2:], true)

	if at != wholeFrames {
		t.Fatalf("converting in chunks produced %d frames, converting whole produced %d", at, wholeFrames)
	}
	for i := range at * 2 {
		if one[i] != two[i] {
			t.Fatalf("sample %d differs: whole %v, in chunks %v", i, one[i], two[i])
		}
	}
}

// A real Ogg Clip, streamed through the real decoder and the real resampler from
// 44.1 kHz into the context's 48 kHz, schedules chunks with no hole between
// them. The synthetic tests above say the arithmetic is right; this says the
// arithmetic is what the shipped decoder is wired to.
func TestARealClipStreamsThroughTheDecoderAndTheResampler(t *testing.T) {
	f := fakeAudio(t)
	b := startedWith(t, f, true, jssound.Config{DecodedClipLimit: alwaysStream})
	id, clip := streamed(t, f, b, fixture(t))
	if clip.(*clipData).filter == nil {
		t.Fatal("a 44.1 kHz Clip against a 48 kHz context kept no resampler")
	}

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Loop: true, Params: sound.VoiceParams{Gains: identity, Rate: 1},
	}}})
	for range 10 {
		f.yield()
		b.Emit(&sound.Batch{})
		f.advance(1.0 / 60)
	}

	sources := f.sources()
	if len(sources) < 8 {
		t.Fatalf("a real streamed Clip scheduled %d chunks, want a schedule worth", len(sources))
	}
	var next float64
	for i, source := range sources {
		buffer := source.Get("buffer")
		if rate := buffer.Get("sampleRate").Int(); rate != streamRate {
			t.Fatalf("chunk %d is at %d Hz, want the context's %d - a seam would interpolate", i, rate, streamRate)
		}
		when := source.Get("started").Get("when").Float()
		if i > 0 && math.Abs(when-next) > 1e-9 {
			t.Fatalf("chunk %d starts at %v, want %v", i, when, next)
		}
		next = when + buffer.Get("length").Float()/streamRate
	}
	if late := b.slots[0].live.late; late != 0 {
		t.Fatalf("%d chunks of a real Clip were late, want none", late)
	}
}
