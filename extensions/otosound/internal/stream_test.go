//go:build !js

package internal

import (
	"bytes"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// The streamed tier's tests, like every other test in this package, render to a
// buffer rather than to a device. What they add is a second thread of the
// Adapter's own - a read-ahead goroutine per Voice - and there is no race
// detector in this environment, so the ones that touch a ring from both sides
// run with -count=10 and say so:
//
//	go test ./extensions/otosound/internal/ -run Streamed -count=10
//
// Their Clips are generated rather than decoded. A generated source is a
// decoder the test can starve, count, stall or make panic outright, which is
// what turns "nothing decodes on the device thread" from a claim into an
// assertion; the real Ogg path is exercised beside them by the fixture.

// generatedSample is one frame of a generated Clip, and says which frame it
// came from exactly, so a rendered block can be compared frame by frame rather
// than by energy.
func generatedSample(frame int64, channel int) float32 {
	return float32((frame+int64(channel)*7)%1000) / 2000
}

// onDeviceGoroutine reports whether the caller is running on the goroutine that
// is pulling blocks out of the Mixer - this suite's device thread. It reads its
// own stack, which is the only honest way to ask: Go has no goroutine identity
// to compare, and a flag set around the call would catch a read-ahead that
// happened to run at the same time rather than one that ran on the wrong
// thread.
func onDeviceGoroutine() bool {
	var stack [8192]byte
	trace := stack[:runtime.Stack(stack[:], false)]
	return bytes.Contains(trace, []byte("internal.pullBlocks")) ||
		bytes.Contains(trace, []byte("internal.(*mixer).Read"))
}

// pullBlocks is the device thread: the one place in this file that pulls the
// Mixer, named so that anything reached from inside it can be recognised.
func pullBlocks(t *testing.T, mx *mixer, blocks int) []float32 {
	t.Helper()
	if !onDeviceGoroutine() {
		t.Fatal("the device-thread guard does not see the thread that pulls the Mixer, so it can prove nothing")
	}
	return render(t, mx, blocks)
}

// generatedSource is one decoder over frames a test computed.
type generatedSource struct {
	frames   int64
	channels int
	at       int64
	// guard makes a read from the device thread a panic, which is the
	// obligation this file exists to prove.
	guard bool
	reads atomic.Int64
	seeks atomic.Int64
	// wraps counts the reads that came after the stream ran out and was sought
	// back, which is what a looping read-ahead does.
	wraps atomic.Int64
}

func (g *generatedSource) read(dst []float32) (int, error) {
	if g.guard && onDeviceGoroutine() {
		panic("a decoder was called on the thread that fills the device buffer")
	}
	g.reads.Add(1)
	if g.at >= g.frames {
		return 0, io.EOF
	}
	frames := min(int64(len(dst)/g.channels), g.frames-g.at)
	for i := range frames {
		for c := range g.channels {
			dst[int(i)*g.channels+c] = generatedSample(g.at+i, c)
		}
	}
	g.at += frames
	return int(frames) * g.channels, nil
}

func (g *generatedSource) seek(frame int64) error {
	g.seeks.Add(1)
	if g.at >= g.frames {
		g.wraps.Add(1)
	}
	g.at = frame
	return nil
}

// generator is a streamed Clip's decoder factory: one source per Voice, kept so
// that a test can ask what its Voice's read-ahead did.
type generator struct {
	mu       sync.Mutex
	frames   int64
	channels int
	guard    bool
	opened   []*generatedSource
}

func (g *generator) open(assets.Blob) (source, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	opened := &generatedSource{frames: g.frames, channels: g.channels, guard: g.guard}
	g.opened = append(g.opened, opened)
	return opened, nil
}

func (g *generator) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.opened)
}

func (g *generator) source(t *testing.T, i int) *generatedSource {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if i >= len(g.opened) {
		t.Fatalf("the Voice's read-ahead opened %d decoders, want more than %d", len(g.opened), i)
	}
	return g.opened[i]
}

// clip is the streamed Clip over this generator, already at the device rate so
// that a rendered frame is the frame the source produced and nothing has been
// filtered in between.
func (g *generator) clip() *clipData {
	if g.channels == 0 {
		g.channels = 1
	}
	return &clipData{
		frames:       int(g.frames),
		channels:     g.channels,
		rate:         testRate,
		sourceRate:   testRate,
		sourceFrames: g.frames,
		duration:     float32(g.frames) / testRate,
		open:         g.open,
	}
}

// primedStream starts a read-ahead and waits for it to finish, so that what
// follows measures the device thread alone. A read-ahead still running is a
// goroutine allocating beside the measurement, and AllocsPerRun counts the
// process rather than the caller.
func primedStream(t *testing.T, clip *clipData) *stream {
	t.Helper()
	s := newStream(clip, 0, false)
	t.Cleanup(s.halt)
	waitFor(t, "the read-ahead to fill the ring", func() bool {
		select {
		case <-s.done:
			return true
		default:
			return false
		}
	})
	return s
}

// Neither tier alone is defensible, so which one a Clip lands in is decided by
// its decoded size against the Config's limit - and the Clip cannot tell,
// because both tiers answer the same four questions with the same four numbers.
//
// The fixture is 48704 frames of 44.1 kHz stereo: 389 KB decoded, under the
// 512 KiB default and over a limit set below it.
func TestAClipUnderTheLimitIsResidentAndOneOverItStreams(t *testing.T) {
	encoded := fixture(t)

	resident := newTestBackend(t, &fakeAudio{})
	resident.Prepare(nil, encoded)
	held := waitPrepared(t, resident)

	streamed := newBackend(otosound.Config{DecodedClipLimit: 64 << 10}, &fakeAudio{})
	t.Cleanup(streamed.stop)
	streamed.Voices(8)
	streamed.Prepare(nil, encoded)
	read := waitPrepared(t, streamed)

	if held.Err != nil || read.Err != nil {
		t.Fatalf("preparing the fixture: resident %v, streamed %v", held.Err, read.Err)
	}
	one, other := held.Clip.(*clipData), read.Clip.(*clipData)
	if one.streams() {
		t.Fatalf("a %d byte Clip streamed under a %d byte limit", clipFrames*clipChannels*4, defaultDecodedClipLimit)
	}
	if !other.streams() {
		t.Fatalf("a %d byte Clip was held resident under a %d byte limit", clipFrames*clipChannels*4, 64<<10)
	}

	// The four facts sound is given, which is all it is given.
	if one.Duration() != other.Duration() || one.Channels() != other.Channels() ||
		one.SampleRate() != other.SampleRate() || one.LoopRegion().Present() != other.LoopRegion().Present() {
		t.Fatalf("the tiers report one Clip as %v/%d/%d and %v/%d/%d",
			one.Duration(), one.Channels(), one.SampleRate(),
			other.Duration(), other.Channels(), other.SampleRate())
	}
	if one.Duration() != clipDuration {
		t.Fatalf("the resident Clip reports %v, want the file's %v", one.Duration(), clipDuration)
	}

	// A resident Clip drops the bytes it decoded; a streamed one keeps them,
	// and keeps the same run rather than a copy of it - a Blob is a pointer
	// and a length, so it references what the asset cache already holds.
	if one.encoded.Len() != 0 {
		t.Fatal("a decoded Clip is still holding its encoded bytes, which nothing will read again")
	}
	if other.encoded != encoded {
		t.Fatal("a streamed Clip is holding a copy of its bytes rather than the run the cache holds")
	}
	if len(other.samples) != 0 {
		t.Fatalf("a streamed Clip is holding %d samples, which is the tier it was not", len(other.samples))
	}
}

// The real Ogg, really streamed: a decoder opened against the retained bytes,
// decoding ahead into a ring, resampling 44.1 kHz to the device's 48 kHz on the
// way - and coming out frame for frame identical to the same Clip held
// resident.
//
// That identity is the tier being invisible, stated as the strongest thing that
// can be said about it. The resident Clip is converted once, in one call; the
// streamed one is converted a few thousand frames at a time on a goroutine, and
// a Clip must not sound different for having been long enough to stream.
func TestAStreamedClipIsFrameForFrameWhatTheResidentOneWouldHaveBeen(t *testing.T) {
	encoded := fixture(t)
	resident, err := decode(encoded, defaultSampleRate, clipFrames)
	if err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	streamed, err := prepare(encoded, defaultSampleRate, 64<<10)
	if err != nil {
		t.Fatalf("preparing the fixture as a stream: %v", err)
	}
	if !streamed.streams() {
		t.Fatal("the fixture was held resident under a limit below its decoded size")
	}

	read := newStream(streamed, 0, false)
	t.Cleanup(read.halt)
	const frames = 8 * testBlock
	waitFor(t, "the read-ahead to fill eight blocks", func() bool { return read.ring.held() >= frames })

	for at := range uint64(frames) {
		for c := range clipChannels {
			want := resident.samples[at*clipChannels+uint64(c)]
			if got := read.ring.at(at, c, clipChannels); got != want {
				t.Fatalf("streamed frame %d channel %d is %v, and resident it is %v", at, c, got, want)
			}
		}
	}
}

// The sentinels, which are one field rather than two that could disagree.
func TestTheResidencyLimitsSentinelsChooseTheTierOutright(t *testing.T) {
	const small, large = int64(1024), int64(4 << 20)
	if overLimit(alwaysStream, small) != true || overLimit(alwaysStream, 0) != true {
		t.Fatal("-2 is always stream, whatever the size")
	}
	if overLimit(neverStream, large) != false {
		t.Fatal("-1 is never stream, whatever the size")
	}
	if overLimit(0, defaultDecodedClipLimit) || !overLimit(0, defaultDecodedClipLimit+1) {
		t.Fatal("0 is the 512 KiB default, and the limit is the largest size still held resident")
	}
	if overLimit(2048, small) || !overLimit(1023, small) {
		t.Fatal("a positive limit is a size in bytes")
	}
}

// A Clip whose length reads zero has no computable decoded size and therefore
// streams. What it must not do is report no duration: a zero duration makes the
// playhead fiction and silently removes ReasonFinished from every Voice that
// names the Clip, so the frames are counted rather than believed.
func TestAStreamWhoseLengthReadsZeroIsCountedRatherThanBelieved(t *testing.T) {
	clip, err := retain(fixture(t), defaultSampleRate, clipRate, clipChannels, 0)
	if err != nil {
		t.Fatalf("retaining a Clip whose length reads zero: %v", err)
	}
	if !clip.streams() {
		t.Fatal("a Clip with no computable decoded size was held resident")
	}
	if clip.sourceFrames != clipFrames {
		t.Fatalf("the scan counted %d frames, want the file's %d", clip.sourceFrames, clipFrames)
	}
	if clip.Duration() != clipDuration {
		t.Fatalf("the Clip reports %v, want the file's %v", clip.Duration(), clipDuration)
	}
}

// The obligation the whole tier is shaped around. A streamed Voice decodes on a
// goroutine of its own and the device thread copies out of a ring; if anything
// ever reached a decoder from inside a block, this decoder would panic on the
// thread that did it.
//
// The guard is checked against the thread it is meant to catch before anything
// is asserted with it, in pullBlocks, so a test that proves nothing fails
// rather than passes.
func TestNothingDecodesOnTheDeviceThreadForAStreamedVoice(t *testing.T) {
	gen := &generator{frames: 8 * testBlock, guard: true}
	b := newTestBackend(t, &fakeAudio{})
	id, err := b.Install(gen.clip())
	if err != nil {
		t.Fatalf("installing a streamed Clip: %v", err)
	}

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}})
	read := b.streams[0]
	if read == nil {
		t.Fatal("a start on a streamed Clip opened no read-ahead")
	}
	waitFor(t, "the read-ahead to produce frames", func() bool { return read.ring.held() > 0 })

	out := left(pullBlocks(t, b.mixer, 4))

	if gen.source(t, 0).reads.Load() == 0 {
		t.Fatal("no decode happened at all, so nothing was proved about where they happen")
	}
	var heard bool
	for _, sample := range out {
		if sample != 0 {
			heard = true
			break
		}
	}
	if !heard {
		t.Fatal("the streamed Voice produced nothing but silence")
	}
}

// What a streamed Voice plays is what its read-ahead decoded, frame for frame:
// the ring is a buffer of the Clip's own frames and the device thread indexes
// it exactly as it indexes a resident Clip's samples.
func TestAStreamedVoicePlaysTheFramesItsReadAheadDecoded(t *testing.T) {
	gen := &generator{frames: 8 * testBlock}
	b := newTestBackend(t, &fakeAudio{})
	id, err := b.Install(gen.clip())
	if err != nil {
		t.Fatalf("installing a streamed Clip: %v", err)
	}

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}})
	read := b.streams[0]
	waitFor(t, "the read-ahead to fill a block", func() bool { return read.ring.held() >= testBlock })

	out := left(pullBlocks(t, b.mixer, 1))
	for i, got := range out {
		if want := generatedSample(int64(i), 0); got != want {
			t.Fatalf("frame %d of the stream is %v, want %v", i, got, want)
		}
	}
}

// One Voice that could not keep up must never take the rest of the mix with it.
// The starved slot contributes silence and every other slot is untouched, which
// is the difference between one Clip stuttering and the whole game going quiet.
func TestAnUnderrunIsSilenceForThatSlotAndNotForTheBlock(t *testing.T) {
	mx, handoff := newTestMixer(4)
	starved := newPCMRing(testBlock, 1)
	// One frame, so the Voice primes and then runs out: an underrun rather
	// than a Voice that has not begun.
	starved.push([]float32{0.25})

	handoff.record(op{
		kind: opStart, slot: 0, clip: &clipData{frames: 4 * testBlock, channels: 1, rate: testRate},
		ring: starved, params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.record(op{
		kind: opStart, slot: 1, clip: constantClip(4*testBlock, 1, 0.5),
		params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.publish()

	out := left(render(t, mx, 1))
	if out[0] != 0.25+0.5 {
		t.Fatalf("the first frame is %v, want the streamed frame and the resident one together", out[0])
	}
	for i := 1; i < len(out); i++ {
		if out[i] != 0.5 {
			t.Fatalf("frame %d of a block with one starved Voice is %v, want the other Voice alone", i, out[i])
		}
	}
	if !mx.voices[0].active {
		t.Fatal("an underrun ended the Voice, and a Voice that could not keep up has not finished")
	}
	// The playhead moved through the gap. A Voice that has begun sounding
	// follows the Device's own rule - a playhead advances whether or not anyone
	// can hear it - so an underrun costs the frames it covered and nothing
	// after them, rather than drifting behind the world for the rest of the
	// Clip.
	if at := mx.voices[0].pos; at != testBlock {
		t.Fatalf("the starved playhead is at %v after a full block, want %d", at, testBlock)
	}
}

// A Voice whose ring has not primed yet holds its playhead rather than
// advancing through silence. That is the Clip-load rule rather than the Device
// rule, and the difference is deliberate: the first fill is a bounded hiccup
// the engine is actively fixing, so the Voice starts from its offset with no
// catch-up, where a Device that may never come back must not stop the world's
// clock. It is also what keeps a streamed one-shot from reaching its end before
// sound's Stop does and being cut instead of ramped.
func TestAStreamedVoiceHoldsItsPlayheadUntilItsRingPrimes(t *testing.T) {
	mx, handoff := newTestMixer(4)
	ring := newPCMRing(4*testBlock, 1)
	clip := &clipData{frames: 4 * testBlock, channels: 1, rate: testRate}
	handoff.record(op{
		kind: opStart, slot: 0, clip: clip, ring: ring,
		params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.publish()

	for i, sample := range render(t, mx, 1) {
		if sample != 0 {
			t.Fatalf("an unprimed Voice produced %v at %d", sample, i)
		}
	}
	if at := mx.voices[0].pos; at != 0 {
		t.Fatalf("the playhead of a Voice that has not begun moved to %v", at)
	}

	frames := make([]float32, testBlock)
	for i := range frames {
		frames[i] = generatedSample(int64(i), 0)
	}
	ring.push(frames)

	out := left(render(t, mx, 1))
	for i, got := range out {
		if want := generatedSample(int64(i), 0); got != want {
			t.Fatalf("frame %d after the ring primed is %v, want the head of the stream %v", i, got, want)
		}
	}
}

// A streamed one-shot ends where its stream ran out. The read-ahead publishes
// the length when it reaches the end of the Clip, which is the only thing that
// knows it; until then the length reads as unreachable, which is what a looping
// Voice - a stream with no end - leaves it at forever.
func TestAStreamedVoiceEndsWhenItsReadAheadSaysTheStreamRanOut(t *testing.T) {
	const frames = testBlock / 2
	mx, handoff := newTestMixer(4)
	ring := newPCMRing(4*testBlock, 1)
	filled := make([]float32, frames)
	for i := range filled {
		filled[i] = 0.5
	}
	ring.push(filled)
	if ring.length() != unknownLength {
		t.Fatal("a stream that has not ended is reporting a length")
	}
	ring.finish()

	handoff.record(op{
		kind: opStart, slot: 0, clip: &clipData{frames: 4 * testBlock, channels: 1, rate: testRate},
		ring: ring, params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.publish()

	out := left(render(t, mx, 1))
	for i := frames; i < len(out); i++ {
		if out[i] != 0 {
			t.Fatalf("the streamed one-shot was still sounding %d frames past its end", i-frames)
		}
	}
	if mx.voices[0].active {
		t.Fatal("the slot was still active after the stream ran out")
	}
}

// A read-ahead stops with its Voice, however the Voice ended. Stolen, released,
// finished or cut by a despawn, all four reach the Adapter as the same stop,
// which is why stopping on it is the whole of the rule.
func TestTheReadAheadStopsWithItsVoice(t *testing.T) {
	gen := &generator{frames: 64 * testBlock}
	b := newTestBackend(t, &fakeAudio{})
	id, err := b.Install(gen.clip())
	if err != nil {
		t.Fatalf("installing a streamed Clip: %v", err)
	}
	start := &sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}}

	b.Emit(start)
	played := b.streams[0]
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}})
	waitFor(t, "the stopped Voice's read-ahead to return", func() bool {
		select {
		case <-played.done:
			return true
		default:
			return false
		}
	})
	if b.streams[0] != nil {
		t.Fatal("the stopped slot is still holding a read-ahead")
	}

	// A steal is a stop and a start on the same slot, and a recovery after a
	// Device loss is a start on a slot that never stopped. Both leave a
	// read-ahead filling a ring nothing will read again, so the start halts
	// whatever was there.
	b.Emit(start)
	stolen := b.streams[0]
	b.Emit(start)
	waitFor(t, "the replaced Voice's read-ahead to return", func() bool {
		select {
		case <-stolen.done:
			return true
		default:
			return false
		}
	})
	if b.streams[0] == stolen {
		t.Fatal("a restarted slot kept the read-ahead the start replaced")
	}
	waitFor(t, "a decoder per start, and one per start only", func() bool { return gen.count() == 3 })
}

// The obligation most likely to fail quietly, now with a streamed Voice in the
// table: the device thread may not allocate, and a ring and a goroutine per
// Voice must not change that. The ring was made at the Voice's start and the
// frames in it were decoded and converted by somebody else, so what is left
// here is two atomic loads and a copy.
//
// The read-ahead is finished before the measurement rather than running beside
// it, because AllocsPerRun counts the process and a decoder allocating on
// another goroutine would be charged to a block it never touched.
func TestTheDeviceThreadAllocatesNothingWithAStreamedVoice(t *testing.T) {
	gen := &generator{frames: 4 * testBlock}
	clip := gen.clip()
	mx, handoff := newTestMixer(8)
	read := primedStream(t, clip)

	handoff.record(op{
		kind: opStart, slot: 0, clip: clip, ring: read.ring,
		params: sound.VoiceParams{Gains: mono(0.8), Rate: 1},
	}, false)
	handoff.publish()
	buf := make([]byte, testBlock*bytesPerFrame)
	if _, err := mx.Read(buf); err != nil {
		t.Fatalf("the Mixer failed a Read: %v", err)
	}

	allocations := testing.AllocsPerRun(200, func() {
		handoff.record(op{
			kind:   opUpdate,
			slot:   0,
			params: sound.VoiceParams{Gains: mono(0.7), Rate: 1},
		}, false)
		handoff.publish()
		if _, err := mx.Read(buf); err != nil {
			t.Fatalf("the Mixer failed a Read: %v", err)
		}
	})

	if allocations != 0 {
		t.Fatalf("a block with a streamed Voice allocated %v times, and the device thread may not allocate at all", allocations)
	}
}

// A looping streamed Voice wraps in the read-ahead rather than in the Mixer:
// the goroutine seeks back and keeps feeding one continuous stream, so the
// device thread's playhead only ever moves forwards and the wrap costs it
// nothing. What comes out is the Clip's frames again, with none repeated and
// none dropped.
func TestAStreamedLoopWrapsInTheReadAheadAndNeverEnds(t *testing.T) {
	const frames = 300
	gen := &generator{frames: frames}
	clip := gen.clip()
	read := newStream(clip, 0, true)
	t.Cleanup(read.halt)

	waitFor(t, "the read-ahead to wrap", func() bool { return read.ring.held() > 3*frames })
	if read.ring.length() != unknownLength {
		t.Fatal("a looping stream published a length, and a loop has no end")
	}

	for at := uint64(0); at < 3*frames; at++ {
		want := generatedSample(int64(at%frames), 0)
		if got := read.ring.at(at, 0, 1); got != want {
			t.Fatalf("frame %d of the looping stream is %v, want %v - the wrap repeated or dropped a frame",
				at, got, want)
		}
	}
	if wraps := gen.source(t, 0).wraps.Load(); wraps < 2 {
		t.Fatalf("the read-ahead wrapped %d times in three passes of the Clip", wraps)
	}
}

// A Voice started at an offset - which is what a Seek is, and what a recovery
// after a Device loss restates every live Voice as - opens its decoder at that
// offset. The seek and the 460 us decoder open behind it happen on the
// read-ahead goroutine, so a burst of them costs the device thread nothing but
// the silence of a ring that has not primed yet.
func TestAStreamedVoiceStartedAtAnOffsetSeeksInItsReadAhead(t *testing.T) {
	const frames, offset = 48000, time.Second / 2
	gen := &generator{frames: frames}
	clip := gen.clip()
	read := newStream(clip, offset, false)
	t.Cleanup(read.halt)

	waitFor(t, "the read-ahead to produce frames", func() bool { return read.ring.held() > 0 })

	at := int64(offset.Seconds() * testRate)
	for i := range int64(64) {
		want := generatedSample(at+i, 0)
		if got := read.ring.at(uint64(i), 0, 1); got != want {
			t.Fatalf("frame %d of a stream opened at %v is %v, want the Clip's frame %d, %v",
				i, offset, got, at+i, want)
		}
	}
	if seeks := gen.source(t, 0).seeks.Load(); seeks != 1 {
		t.Fatalf("a start at an offset sought %d times", seeks)
	}
}
