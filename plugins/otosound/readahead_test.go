//go:build !js

package otosound

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The streamed tier's tests render to a buffer, as every test in this package
// does. What they add is a goroutine of otosound's own per voice, the
// read-ahead, so the ones that touch a ring from both sides run under -race.
// Their clips are generated rather than decoded: a generated decoder is one a
// test can count, starve, or make panic if the device thread calls it, which
// turns nothing decoding there from a claim into an assertion. The real Ogg
// path is exercised beside them by the fixture.

// testRefillWait is how long a test's read-ahead sleeps on a full ring: short,
// so a test waiting for frames waits for little else.
const testRefillWait = time.Millisecond

// isOnDeviceGoroutine is whether the caller runs on the goroutine pulling
// blocks out of the mixer, this suite's device thread. It reads its own stack,
// since Go has no goroutine identity to compare, and a flag set around the
// call would catch a read-ahead that happened to run at the same time rather
// than one that ran on the wrong thread.
func isOnDeviceGoroutine() bool {
	var stack [8192]byte
	trace := stack[:runtime.Stack(stack[:], false)]
	return bytes.Contains(trace, []byte("otosound.pullBlocks")) || bytes.Contains(trace, []byte("otosound.(*mixer).Read"))
}

// pullBlocks is the device thread: the one place in these tests that pulls
// the mixer, named so that anything reached from inside it is recognised. It
// fails the test when the guard cannot see it, since a guard that cannot see
// the device thread proves nothing.
func pullBlocks(t *testing.T, source io.Reader, blocks int) []float32 {
	t.Helper()
	if !isOnDeviceGoroutine() {
		t.Fatal("the device thread's guard does not see the thread that pulls the mixer, so it proves nothing")
	}
	return renderBlocks(t, source, blocks)
}

// startReadAhead starts the read-ahead of a voice on clip at offset, its ring
// sized for ringFrames, as Emit starts one, and halts it and waits for it to
// return when the test ends.
func startReadAhead(t *testing.T, clip *preparedClip, offset time.Duration, loop bool, ringFrames int) *readAhead {
	t.Helper()
	startedReadAhead := newReadAhead(clip, offset, loop, ringFrames)
	go startedReadAhead.fill(testRefillWait)
	t.Cleanup(func() {
		startedReadAhead.halt()
		<-startedReadAhead.done
	})
	return startedReadAhead
}

// waitFor fails the test unless done answers true within sfxtest.Timeout.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(sfxtest.Timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %v for %s", sfxtest.Timeout, what)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitReturned fails the test unless startedReadAhead's goroutine returns within
// sfxtest.Timeout.
func waitReturned(t *testing.T, what string, startedReadAhead *readAhead) {
	t.Helper()
	waitFor(t, what, func() bool {
		select {
		case <-startedReadAhead.done:
			return true
		default:
			return false
		}
	})
}

// isHalted is whether startedReadAhead has been told to stop.
func isHalted(startedReadAhead *readAhead) bool {
	select {
	case <-startedReadAhead.halted:
		return true
	default:
		return false
	}
}

// ringSample answers one channel of one frame ring holds.
func ringSample(ring *readAheadRing, frame uint64, channel int) float32 {
	return ring.samples[ring.sampleIndex(frame)+channel]
}

// makeStreamedStart answers the start of a voice in slot reading ring, its
// frames clip's, at gains and the clip's own rate.
func makeStreamedStart(slot sfx.VoiceSlot, clip *preparedClip, ring *readAheadRing, gains [2][2]float32, loop bool) operation {
	start := makeStart(slot, clip, gains, loop)
	start.ring = ring
	return start
}

// installStreamed installs clip through b's Install, as sfx installs a
// prepare's completion, and answers its ID.
func installStreamed(t *testing.T, b backend, clip *preparedClip) sfx.ClipID {
	t.Helper()
	clipID, err := b.Install(clip)
	if err != nil {
		t.Fatalf("installing a streamed clip: %v", err)
	}
	return clipID
}

// makeTestStreamingBackend answers the backend of an otosound with a short
// RefillWait over a fake device that never pulls, so a test pulls its mixer by
// hand.
func makeTestStreamingBackend(t *testing.T, config Config) backend {
	t.Helper()
	config.RefillWait = testRefillWait
	return makeTestBackend(t, config)
}

// makeStartChanges answers one flush's changes starting a voice on clipID in
// slot, at full mono gain.
func makeStartChanges(slot sfx.VoiceSlot, clipID sfx.ClipID, loop bool) *sfx.FlushChanges {
	return &sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: slot, Clip: clipID, Loop: loop, Params: sfx.VoiceParams{Gains: makeMonoGains(1), Rate: 1}}}}
}

// The real Ogg, really streamed: a decoder opened over the clip's bytes,
// decoding ahead into a ring and converting 44.1 kHz to the device's 48 kHz on
// the way, comes out frame for frame what the same clip held resident is, its
// tail drained from the filter's window and its length published where the
// resident clip ends. The resident clip is converted in one call; the
// streamed one a chunk at a time on a goroutine, and a clip must not sound
// different for being long enough to stream.
func TestAStreamedClipIsFrameForFrameWhatTheResidentOneWouldHaveBeen(t *testing.T) {
	resident, err := prepareClip(readFixture(t), decodedClipLimitNone, defaultSampleRate)
	if err != nil {
		t.Fatalf("preparing the fixture resident: %v", err)
	}
	streamed, err := prepareClip(readFixture(t), decodedClipLimitAlwaysStream, defaultSampleRate)
	if err != nil {
		t.Fatalf("preparing the fixture to stream: %v", err)
	}
	heldFrames := uint64(len(resident.samples) / clipChannels)

	voiceReadAhead := startReadAhead(t, streamed, 0, false, 1<<16)
	waitReturned(t, "the read-ahead to reach the clip's end", voiceReadAhead)

	if length := voiceReadAhead.ring.streamLength(); length != heldFrames {
		t.Fatalf("the read-ahead published a length of %d frames, and the resident clip holds %d", length, heldFrames)
	}
	for frame := range heldFrames {
		for channel := range clipChannels {
			want := resident.samples[frame*clipChannels+uint64(channel)]
			if got := ringSample(voiceReadAhead.ring, frame, channel); got != want {
				t.Fatalf("streamed frame %d channel %d is %v, and resident it is %v", frame, channel, got, want)
			}
		}
	}
}

// The obligation the tier is shaped around. A streamed voice decodes on a
// goroutine of its own and the device thread copies out of a ring; if
// anything ever reached a decoder from inside a block, this decoder would
// panic on the thread that did it.
func TestNothingDecodesOnTheDeviceThreadForAStreamedVoice(t *testing.T) {
	stream := &generatedStream{frames: 8 * testBlock, guarded: true}
	b := makeTestStreamingBackend(t, Config{})
	clipID := installStreamed(t, b, stream.clip())

	b.Emit(makeStartChanges(0, clipID, false))
	voiceReadAhead := b.plugin.readAheads[0]
	if voiceReadAhead == nil {
		t.Fatal("a start on a streamed clip opened no read-ahead")
	}
	waitFor(t, "the read-ahead to produce frames", func() bool { return voiceReadAhead.ring.producedFrames() > 0 })

	rendered := pickLeft(pullBlocks(t, &b.plugin.mixer, 4))
	if stream.decoder(t, 0).reads.Load() == 0 {
		t.Fatal("nothing was decoded at all, so nothing is proved about where it was")
	}
	heard := false
	for _, sample := range rendered {
		heard = heard || sample != 0
	}
	if !heard {
		t.Fatal("the streamed voice played nothing but silence")
	}
}

// What a streamed voice plays is what its read-ahead decoded, frame for
// frame: the ring holds the clip's own frames, and the device thread indexes
// it as it indexes a resident clip's samples.
func TestAStreamedVoicePlaysTheFramesItsReadAheadDecoded(t *testing.T) {
	stream := &generatedStream{frames: 8 * testBlock}
	b := makeTestStreamingBackend(t, Config{})
	clipID := installStreamed(t, b, stream.clip())

	b.Emit(makeStartChanges(0, clipID, false))
	voiceReadAhead := b.plugin.readAheads[0]
	waitFor(t, "the read-ahead to fill a block", func() bool { return voiceReadAhead.ring.producedFrames() >= testBlock })

	for frame, sample := range pickLeft(pullBlocks(t, &b.plugin.mixer, 1)) {
		if want := generatedSample(int64(frame), 0); sample != want {
			t.Fatalf("frame %d of the stream is %v, want %v", frame, sample, want)
		}
	}
}

// One voice that could not keep up must never take the rest of the mix with
// it: the starved slot plays silence and every other slot is untouched. Once
// a voice has sounded, its playhead moves on through the gap, a playhead
// advancing whether or not anyone can hear it, so an underrun costs the
// frames it covered and does not leave the voice behind the world for good.
func TestAnUnderrunIsSilenceForThatSlotAndNotForTheBlock(t *testing.T) {
	testMixer := makeTestMixer(4)
	starved := newReadAheadRing(testBlock, 1)
	// One frame, so the voice primes and then runs out: an underrun rather
	// than a voice that has not begun.
	starved.push([]float32{0.25})
	streamedClip := &preparedClip{frames: 4 * testBlock, channels: 1, sampleRate: testRate}
	publishOperations(testMixer,
		makeStreamedStart(0, streamedClip, starved, makeMonoGains(1), false),
		makeStart(1, makeConstantClip(4*testBlock, 1, 0.5), makeMonoGains(1), false),
	)

	rendered := pickLeft(renderBlocks(t, testMixer, 1))
	if rendered[0] != 0.25+0.5 {
		t.Fatalf("the first frame is %v, want the streamed frame and the resident one together", rendered[0])
	}
	for frame := 1; frame < len(rendered); frame++ {
		if rendered[frame] != 0.5 {
			t.Fatalf("frame %d of a block with one starved voice is %v, want the other voice alone", frame, rendered[frame])
		}
	}
	if testMixer.voices[0].clip == nil {
		t.Fatal("an underrun ended the voice, and a voice that could not keep up has not finished")
	}
	if playhead := testMixer.voices[0].playhead; playhead != testBlock {
		t.Fatalf("the starved playhead is at %v after a full block, want %d", playhead, testBlock)
	}
}

// A voice whose ring has not primed holds its playhead rather than advancing
// through silence: the first fill is a clip load, which starts from its
// offset with no catch-up. It is also what keeps a streamed one-shot from
// reaching its end before sfx's stop does and being cut instead of ramped.
func TestAStreamedVoiceHoldsItsPlayheadUntilItsRingPrimes(t *testing.T) {
	testMixer := makeTestMixer(4)
	ring := newReadAheadRing(4*testBlock, 1)
	streamedClip := &preparedClip{frames: 4 * testBlock, channels: 1, sampleRate: testRate}
	publishOperations(testMixer, makeStreamedStart(0, streamedClip, ring, makeMonoGains(1), false))

	for frame, sample := range renderBlocks(t, testMixer, 1) {
		if sample != 0 {
			t.Fatalf("a voice whose ring has not primed played %v at %d", sample, frame)
		}
	}
	if playhead := testMixer.voices[0].playhead; playhead != 0 {
		t.Fatalf("the playhead of a voice that has not begun moved to %v", playhead)
	}

	head := make([]float32, testBlock)
	for frame := range head {
		head[frame] = generatedSample(int64(frame), 0)
	}
	ring.push(head)
	for frame, sample := range pickLeft(renderBlocks(t, testMixer, 1)) {
		if want := generatedSample(int64(frame), 0); sample != want {
			t.Fatalf("frame %d after the ring primed is %v, want the head of the stream's %v", frame, sample, want)
		}
	}
}

// A streamed one-shot ends where its stream ran out. The read-ahead publishes
// the length on reaching the clip's end, the only thing that knows it; until
// then the length reads as unreachable, which a looping voice leaves it at
// for good.
func TestAStreamedVoiceEndsWhenItsReadAheadSaysTheStreamRanOut(t *testing.T) {
	const frames = testBlock / 2
	testMixer := makeTestMixer(4)
	ring := newReadAheadRing(4*testBlock, 1)
	ring.push(slices.Repeat([]float32{0.5}, frames))
	if ring.streamLength() != unknownStreamLength {
		t.Fatal("a stream that has not ended reports a length")
	}
	ring.length.Store(ring.produced.Load())
	streamedClip := &preparedClip{frames: 4 * testBlock, channels: 1, sampleRate: testRate}
	publishOperations(testMixer, makeStreamedStart(0, streamedClip, ring, makeMonoGains(1), false))

	rendered := pickLeft(renderBlocks(t, testMixer, 1))
	for frame := frames; frame < len(rendered); frame++ {
		if rendered[frame] != 0 {
			t.Fatalf("the streamed one-shot still sounded %d frames past its end", frame-frames)
		}
	}
	if testMixer.voices[0].clip != nil {
		t.Fatal("the slot still plays after its stream ran out")
	}
}

// A decoder that will not open ends the voice and reports nothing: the clip
// parsed at its prepare, and the game has been told everything it is owed.
// It gets silence and an ending, as any voice that ran out does.
func TestAStreamedVoiceWhoseDecoderWillNotOpenEndsInSilence(t *testing.T) {
	stream := &generatedStream{frames: 8 * testBlock, openErr: errors.New("the decoder will not open")}
	b := makeTestStreamingBackend(t, Config{})
	clipID := installStreamed(t, b, stream.clip())

	b.Emit(makeStartChanges(0, clipID, false))
	voiceReadAhead := b.plugin.readAheads[0]
	waitReturned(t, "the read-ahead to give up", voiceReadAhead)

	for frame, sample := range pullBlocks(t, &b.plugin.mixer, 1) {
		if sample != 0 {
			t.Fatalf("a voice whose decoder would not open played %v at %d", sample, frame)
		}
	}
	if b.plugin.mixer.voices[0].clip != nil {
		t.Fatal("a voice whose decoder would not open still plays")
	}
}

// A read-ahead stops with its voice, however the voice ended: finished,
// stolen, released or cut, each reaches otosound as the same stop. A steal is
// a stop and a start on one slot, and a recovery restates a live voice as a
// start on a slot that never stopped, so a start halts whatever read-ahead
// the slot had too.
func TestTheReadAheadStopsWithItsVoice(t *testing.T) {
	stream := &generatedStream{frames: 64 * testBlock}
	b := makeTestStreamingBackend(t, Config{})
	clipID := installStreamed(t, b, stream.clip())

	b.Emit(makeStartChanges(0, clipID, false))
	playedReadAhead := b.plugin.readAheads[0]
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}})
	waitReturned(t, "the stopped voice's read-ahead to return", playedReadAhead)
	if b.plugin.readAheads[0] != nil {
		t.Fatal("the stopped slot still holds a read-ahead")
	}

	b.Emit(makeStartChanges(0, clipID, false))
	replacedReadAhead := b.plugin.readAheads[0]
	b.Emit(makeStartChanges(0, clipID, false))
	waitReturned(t, "the replaced voice's read-ahead to return", replacedReadAhead)
	if b.plugin.readAheads[0] == replacedReadAhead || b.plugin.readAheads[0] == nil {
		t.Fatal("a restarted slot kept the read-ahead the start replaced, or none")
	}
	waitFor(t, "a decoder per start, and one per start only", func() bool { return stream.openCount() == 3 })
}

// Releasing a streamed clip while it plays is safe as built: sfx stops its
// voices first, in the same flush's changes, and each stop halts its
// voice's read-ahead before the destroy is recorded. The clip is held until
// the mixer has passed the changes that destroyed it, the voices ramping to
// silence out of their rings meanwhile, and its bytes go with the last
// read-ahead reading them.
func TestAStreamedClipReleasedWhileItPlaysStopsItsReadAheadsBeforeItsDestroy(t *testing.T) {
	stream := &generatedStream{frames: 4 * testBlock}
	b := makeTestStreamingBackend(t, Config{})
	clip := stream.clip()
	clipID := installStreamed(t, b, clip)
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{
		{Slot: 0, Clip: clipID, Loop: true, Params: sfx.VoiceParams{Gains: makeMonoGains(0.5), Rate: 1}},
		{Slot: 1, Clip: clipID, Loop: true, Params: sfx.VoiceParams{Gains: makeMonoGains(0.5), Rate: 1}},
	}})
	playingReadAheads := []*readAhead{b.plugin.readAheads[0], b.plugin.readAheads[1]}
	for _, voiceReadAhead := range playingReadAheads {
		waitFor(t, "the read-aheads to fill a block", func() bool { return voiceReadAhead.ring.producedFrames() >= testBlock })
	}
	pullBlocks(t, &b.plugin.mixer, 1)

	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0, 1}, Destroys: []sfx.ClipID{clipID}})
	for slot, voiceReadAhead := range playingReadAheads {
		if !isHalted(voiceReadAhead) || b.plugin.readAheads[slot] != nil {
			t.Fatalf("slot %d's read-ahead was not halted by the stop that came with its clip's destroy", slot)
		}
	}
	if _, installed := b.plugin.clips[clipID]; installed || len(b.plugin.releases) != 1 || b.plugin.releases[0].clip != clip {
		t.Fatal("the released clip is still installed, or is not held while the mixer may read its voices' rings")
	}

	rendered := pickLeft(pullBlocks(t, &b.plugin.mixer, 1))
	if rendered[0] == 0 || rendered[len(rendered)-1] != 0 {
		t.Fatalf("the released voices went from %v to %v, want a ramp from what they played to silence", rendered[0], rendered[len(rendered)-1])
	}
	b.Emit(&sfx.FlushChanges{})
	if len(b.plugin.releases) != 0 {
		t.Fatal("the released clip is still held after the mixer passed its destroy")
	}
	for _, voiceReadAhead := range playingReadAheads {
		waitReturned(t, "a halted read-ahead to return", voiceReadAhead)
	}
}

// A read-ahead ends with its voice, and a kernel being destroyed ends every
// voice there is, whether or not sfx got as far as emitting the stops, so
// Destroy halts every read-ahead and waits for them.
func TestDestroyHaltsEveryReadAhead(t *testing.T) {
	stream := &generatedStream{frames: 4 * testBlock}
	var runningReadAheads []*readAhead
	t.Run("the kernel, destroyed as it ends", func(t *testing.T) {
		b := makeTestStreamingBackend(t, Config{})
		clipID := installStreamed(t, b, stream.clip())
		b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: clipID, Loop: true}, {Slot: 1, Clip: clipID, Loop: true}}})
		runningReadAheads = []*readAhead{b.plugin.readAheads[0], b.plugin.readAheads[1]}
	})
	for slot, voiceReadAhead := range runningReadAheads {
		select {
		case <-voiceReadAhead.done:
		default:
			t.Fatalf("slot %d's read-ahead still runs after Destroy returned", slot)
		}
	}
}

// The obligation most likely to fail quietly, with a streamed voice in the
// table: the device thread may not allocate, and a ring and a goroutine per
// voice must not change that. The read-ahead has finished before the count,
// since AllocsPerRun counts the process, and a decoder allocating on another
// goroutine would be charged to a block that never touched it.
func TestTheDeviceThreadAllocatesNothingWithAStreamedVoice(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	stream := &generatedStream{frames: 4 * testBlock}
	clip := stream.clip()
	voiceReadAhead := startReadAhead(t, clip, 0, false, 8*testBlock)
	waitReturned(t, "the read-ahead to reach the clip's end", voiceReadAhead)
	testMixer := makeTestMixer(8)
	publishOperations(testMixer, makeStreamedStart(0, clip, voiceReadAhead.ring, makeMonoGains(0.8), false))
	block := make([]byte, testBlock*bytesPerFrame)
	testMixer.Read(block)

	allocations := testing.AllocsPerRun(200, func() {
		publishOperations(testMixer, makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(0.7), Rate: 1}))
		testMixer.Read(block)
	})
	if allocations != 0 {
		t.Fatalf("a block with a streamed voice allocated %v times, and the device thread may not allocate at all", allocations)
	}
}

// A looping streamed voice wraps in its read-ahead rather than in the mixer:
// the goroutine seeks back and keeps feeding one continuous stream, so the
// device thread's playhead only ever moves forwards. What comes out is the
// clip's frames again, none repeated and none dropped, and no length.
func TestAStreamedLoopWrapsInTheReadAheadAndNeverEnds(t *testing.T) {
	const frames = 300
	stream := &generatedStream{frames: frames}
	voiceReadAhead := startReadAhead(t, stream.clip(), 0, true, 1<<13)

	waitFor(t, "the read-ahead to wrap", func() bool { return voiceReadAhead.ring.producedFrames() > 3*frames })
	if voiceReadAhead.ring.streamLength() != unknownStreamLength {
		t.Fatal("a looping stream published a length, and a loop has no end")
	}
	for frame := range uint64(3 * frames) {
		if got, want := ringSample(voiceReadAhead.ring, frame, 0), generatedSample(int64(frame%frames), 0); got != want {
			t.Fatalf("frame %d of the looping stream is %v, want %v: the wrap repeated or dropped a frame", frame, got, want)
		}
	}
	if wraps := stream.decoder(t, 0).wraps.Load(); wraps < 2 {
		t.Fatalf("the read-ahead wrapped %d times in three passes of the clip", wraps)
	}
}

// A voice started at an offset, which is what a seek is and what a recovery
// restates every live voice as, opens its decoder at the offset. The seek and
// the decoder's open behind it happen on the read-ahead's goroutine, so a
// burst of them costs the device thread only the silence of a ring that has
// not primed.
func TestAStreamedVoiceStartedAtAnOffsetSeeksInItsReadAhead(t *testing.T) {
	const frames, offset = 48000, time.Second / 2
	stream := &generatedStream{frames: frames}
	voiceReadAhead := startReadAhead(t, stream.clip(), offset, false, 1<<13)

	waitFor(t, "the read-ahead to produce frames", func() bool { return voiceReadAhead.ring.producedFrames() > 0 })
	offsetFrame := int64(offset.Seconds() * testRate)
	for frame := range int64(64) {
		if got, want := ringSample(voiceReadAhead.ring, uint64(frame), 0), generatedSample(offsetFrame+frame, 0); got != want {
			t.Fatalf("frame %d of a stream opened at %v is %v, want the clip's frame %d's %v", frame, offset, got, offsetFrame+frame, want)
		}
	}
	if seeks := stream.decoder(t, 0).seeks.Load(); seeks != 1 {
		t.Fatalf("a start at an offset sought %d times", seeks)
	}
}

// The streamed tier wraps at a loop region in its read-ahead, ahead of where
// the mixer reads: the decoder seeks back to the region's start while it
// fills the ring, so the frames come out of the ring in one unbroken run,
// and the stream never runs out, the region ending before it.
func TestAStreamedLoopWrapsInTheReadAheadAtTheRegionsBounds(t *testing.T) {
	const from, to = 400, 1200
	stream := &generatedStream{frames: 4 * testBlock, guarded: true}
	clip := stream.clip()
	clip.loopRegion = m.Some(sfx.LoopRegion{Start: float32(from) / testRate, End: float32(to) / testRate})
	if start, end := clip.loopBounds(testRate, float64(clip.frames)); start != from || end != to {
		t.Fatalf("the read-ahead would loop [%v, %v), want [%d, %d)", start, end, from, to)
	}

	voiceReadAhead := startReadAhead(t, clip, 0, true, 1<<13)
	const rendered = to + 3*(to-from)
	waitFor(t, "the read-ahead to fill past three wraps", func() bool { return voiceReadAhead.ring.producedFrames() >= rendered })
	for frame := range int64(rendered) {
		sourceFrame := frame
		if sourceFrame >= to {
			sourceFrame = from + (sourceFrame-to)%(to-from)
		}
		if got, want := ringSample(voiceReadAhead.ring, uint64(frame), 0), generatedSample(sourceFrame, 0); got != want {
			t.Fatalf("frame %d of the streamed loop is %v, want source frame %d's %v", frame, got, sourceFrame, want)
		}
	}
	if seeks := stream.decoder(t, 0).seeks.Load(); seeks < 3 {
		t.Fatalf("the read-ahead sought back %d times over three spans of the loop", seeks)
	}
	if wraps := stream.decoder(t, 0).wraps.Load(); wraps != 0 {
		t.Fatalf("the read-ahead ran the stream out %d times, and a loop region ends before it", wraps)
	}
	if voiceReadAhead.ring.streamLength() != unknownStreamLength {
		t.Fatal("a looping stream published its length, and a looping voice never ends by itself")
	}
}

// A real Ogg with a real loop tag, prepared in both tiers, wraps at the same
// frame and comes out frame for frame the same across three passes of its
// loop: a loop point is exactly the kind of fact that would give the tier
// away.
func TestBothTiersWrapARealClipAtTheSameFrame(t *testing.T) {
	const from, length = 1000, 2000
	tagged := retagClip(t, readFixture(t), "LOOPSTART=1000", "LOOPLENGTH=2000")
	resident, err := prepareClip(tagged, decodedClipLimitNone, clipRate)
	if err != nil {
		t.Fatalf("preparing the tagged fixture resident: %v", err)
	}
	streamed, err := prepareClip(tagged, decodedClipLimitAlwaysStream, clipRate)
	if err != nil {
		t.Fatalf("preparing the tagged fixture to stream: %v", err)
	}
	if start, end := streamed.loopBounds(clipRate, float64(streamed.frames)); start != from || end != from+length {
		t.Fatalf("the streamed tier loops [%v, %v), want [%d, %d)", start, end, from, from+length)
	}

	voiceReadAhead := startReadAhead(t, streamed, 0, true, 1<<15)
	const rendered = from + 3*length
	waitFor(t, "the read-ahead to fill past three wraps", func() bool { return voiceReadAhead.ring.producedFrames() >= rendered })
	for frame := range int64(rendered) {
		sourceFrame := frame
		if sourceFrame >= from+length {
			sourceFrame = from + (sourceFrame-from)%length
		}
		for channel := range clipChannels {
			want := resident.samples[sourceFrame*clipChannels+int64(channel)]
			if got := ringSample(voiceReadAhead.ring, uint64(frame), channel); got != want {
				t.Fatalf("frame %d channel %d is %v streamed and %v resident, at source frame %d", frame, channel, got, want, sourceFrame)
			}
		}
	}
}

// The mixer reads a streamed loop as it reads any stream: the ring did the
// wrapping, so the playhead only moves forwards and the voice never ends.
func TestAStreamedLoopIsGaplessThroughTheMixer(t *testing.T) {
	const from, to = 400, 1200
	stream := &generatedStream{frames: 4 * testBlock, guarded: true}
	clip := stream.clip()
	clip.loopRegion = m.Some(sfx.LoopRegion{Start: float32(from) / testRate, End: float32(to) / testRate})
	voiceReadAhead := startReadAhead(t, clip, 0, true, 1<<13)
	waitFor(t, "the read-ahead to fill a block", func() bool { return voiceReadAhead.ring.producedFrames() >= testBlock })

	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStreamedStart(0, clip, voiceReadAhead.ring, makeMonoGains(1), true))
	for frame, sample := range pickLeft(pullBlocks(t, testMixer, 1)) {
		sourceFrame := int64(frame)
		if sourceFrame >= to {
			sourceFrame = from + (sourceFrame-to)%(to-from)
		}
		if want := generatedSample(sourceFrame, 0); sample != want {
			t.Fatalf("frame %d out of the mixer is %v, want source frame %d's %v", frame, sample, sourceFrame, want)
		}
	}
	if testMixer.voices[0].clip == nil {
		t.Fatal("the looping streamed voice ended at its loop region's end")
	}
}

// The read-ahead fills its ring while the mixer reads it, round and round a
// ring far shorter than what plays through it, and every frame the mixer
// plays is the clip's own, none torn, repeated or dropped. Run under -race,
// it fails with the ring's produced count stored before its frames are
// copied.
func TestTheReadAheadFillsItsRingWhileTheMixerReadsIt(t *testing.T) {
	const frames, blocks = 1000, 200
	stream := &generatedStream{frames: frames, guarded: true}
	b := makeTestStreamingBackend(t, Config{ReadAhead: time.Millisecond})
	clipID := installStreamed(t, b, stream.clip())

	b.Emit(makeStartChanges(0, clipID, true))
	voiceReadAhead := b.plugin.readAheads[0]
	if capacity := voiceReadAhead.ring.capacity; capacity*4 > blocks*testBlock {
		t.Fatalf("the ring holds %d frames, too many for %d blocks to wrap it", capacity, blocks)
	}
	for block := range uint64(blocks) {
		// Each block waits for its frames, so what is asserted is the
		// handoff and not an underrun.
		waitFor(t, "the read-ahead to fill the next block", func() bool { return voiceReadAhead.ring.producedFrames() > (block+1)*testBlock })
		for index, sample := range pickLeft(pullBlocks(t, &b.plugin.mixer, 1)) {
			frame := int64(block)*testBlock + int64(index)
			if want := generatedSample(frame%frames, 0); sample != want {
				t.Fatalf("frame %d of the stream is %v, want %v", frame, sample, want)
			}
		}
	}
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}})
	waitReturned(t, "the stopped voice's read-ahead to return", voiceReadAhead)
}
