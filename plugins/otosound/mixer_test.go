//go:build !js

package otosound

import (
	"encoding/binary"
	"io"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The mixer's tests render to a buffer and never to a device, as oto would
// pull it, so every property below is asserted without a sound card.

// testRate and testBlock are the defaults: 48 kHz and a 10 ms block, which is
// also the declick ramp's length, since the ramp is one block.
const (
	testRate  = defaultSampleRate
	testBlock = testRate / 100
)

// makeConstantClip answers a clip at testRate whose every sample is value.
// With a source that never moves, the output is the gain envelope, which is
// the one way to measure a ramp without the waveform burying it.
func makeConstantClip(frames, channels int, value float32) *preparedClip {
	return makeTestClip(channels, slices.Repeat([]float32{value}, frames*channels))
}

// makeRampClip answers a mono clip at testRate whose frames count upwards
// inside [0, 1), so a rendered sample says exactly which frame it came from.
func makeRampClip(frames int) *preparedClip {
	samples := make([]float32, frames)
	for index := range samples {
		samples[index] = float32(index) / float32(frames)
	}
	return makeTestClip(1, samples)
}

// makeTestClip answers a clip at testRate holding samples, channels to a
// frame.
func makeTestClip(channels int, samples []float32) *preparedClip {
	return &preparedClip{samples: samples, frames: int64(len(samples) / channels), channels: channels, sampleRate: testRate}
}

// makeMonoGains answers the matrix sfx computes for a non-positional mono
// voice: row 0 alone, both outputs, scaled by volume.
func makeMonoGains(volume float32) [2][2]float32 {
	return [2][2]float32{{volume, volume}}
}

// makeTestMixer answers a mixer of voiceCount slots over a ring of the
// default depth, made as Voices makes them.
func makeTestMixer(voiceCount int) *mixer {
	testMixer := makeMixer(newHandoffRing(defaultRingDepth, voiceCount), voiceCount)
	return &testMixer
}

// publishOperations publishes operations to testMixer as one flush's changes.
func publishOperations(testMixer *mixer, operations ...operation) {
	for _, recorded := range operations {
		testMixer.ring.staging.record(recorded, false)
	}
	testMixer.ring.publish()
}

// makeStart answers the start of a voice on clip in slot, at gains and the
// clip's own rate.
func makeStart(slot sfx.VoiceSlot, clip *preparedClip, gains [2][2]float32, loop bool) operation {
	return operation{kind: operationKindStart, slot: slot, clip: clip, loop: loop, params: sfx.VoiceParams{Gains: gains, Rate: 1}}
}

// makeUpdate answers the update of slot to params.
func makeUpdate(slot sfx.VoiceSlot, params sfx.VoiceParams) operation {
	return operation{kind: operationKindUpdate, slot: slot, params: params}
}

// renderBlocks reads blocks whole blocks of testBlock frames out of source,
// as a device would, and answers them as interleaved float32.
func renderBlocks(t testing.TB, source io.Reader, blocks int) []float32 {
	t.Helper()
	block := make([]byte, testBlock*bytesPerFrame)
	rendered := make([]float32, 0, blocks*testBlock*outputChannels)
	for range blocks {
		read, err := source.Read(block)
		if err != nil || read != len(block) {
			t.Fatalf("the mixer read %d bytes and %v for a %d byte block", read, err, len(block))
		}
		for at := 0; at < read; at += 4 {
			rendered = append(rendered, math.Float32frombits(binary.LittleEndian.Uint32(block[at:])))
		}
	}
	return rendered
}

// pickLeft answers the left output channel of an interleaved render.
func pickLeft(rendered []float32) []float32 {
	picked := make([]float32, len(rendered)/outputChannels)
	for index := range picked {
		picked[index] = rendered[index*outputChannels]
	}
	return picked
}

// The device thread may not allocate, and every buffer it needs, the ring,
// its change lists, the voice table and the list of sounding slots, was made
// once at Voices. oto's own pull path allocates about 114 times a second,
// which is what this is measured against rather than included in: what
// otosound owes is that its own contribution is zero.
func TestTheDeviceThreadAllocatesNothingOverAFullBlock(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	testMixer := makeTestMixer(8)
	publishOperations(testMixer, makeStart(0, makeConstantClip(4096, 2, 0.5), [2][2]float32{{0.8, 0.2}, {0.2, 0.8}}, true))
	block := make([]byte, testBlock*bytesPerFrame)

	allocations := testing.AllocsPerRun(200, func() {
		// A publish and a drain every block, so the count covers the handoff
		// and not only the arithmetic.
		publishOperations(testMixer, makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(0.7), Rate: 1.5}))
		testMixer.Read(block)
	})
	if allocations != 0 {
		t.Fatalf("a block allocated %v times, and the device thread may not allocate at all", allocations)
	}
}

// sfx emits target values once per flush and otosound ramps to them across
// its own blocks, since declicking is a function of the output block rate,
// which only the backend knows. v1's prototype measured the gain stepping
// 0.078 of amplitude at every block boundary without the ramp, and 0.00016
// with it, smaller than the ramp's own per-sample motion.
func TestAGainChangeRampsAcrossTheBlockRatherThanSteppingAtIt(t *testing.T) {
	const quiet, loud = float32(0.1), float32(0.9)
	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStart(0, makeConstantClip(4*testBlock, 1, 1), makeMonoGains(quiet), false))
	renderBlocks(t, testMixer, 1)

	publishOperations(testMixer, makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(loud), Rate: 1}))
	rendered := pickLeft(renderBlocks(t, testMixer, 1))

	rampStep := float64(loud-quiet) / testBlock
	if atBoundary := math.Abs(float64(rendered[0] - quiet)); atBoundary > rampStep*1.001 {
		t.Fatalf("the gain stepped %v across the block boundary, and the ramp's own step is %v", atBoundary, rampStep)
	}
	for index := 1; index < len(rendered); index++ {
		if inside := math.Abs(float64(rendered[index] - rendered[index-1])); inside > rampStep*1.001 {
			t.Fatalf("the gain stepped %v inside the block at sample %d, and the ramp's own step is %v", inside, index, rampStep)
		}
	}
	if last := rendered[len(rendered)-1]; last != loud {
		t.Fatalf("the block ended on %v rather than exactly on its target %v", last, loud)
	}
}

// A stop is the same mechanism as any other parameter change: a ramp to zero
// over the block, the slot freed only once it has reached silence. This is
// why sfx's ending of a voice precedes its silence by a few milliseconds.
func TestAStoppedVoiceRampsToSilenceBeforeItsSlotIsFreed(t *testing.T) {
	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStart(0, makeConstantClip(4*testBlock, 1, 1), makeMonoGains(1), false))
	renderBlocks(t, testMixer, 1)

	publishOperations(testMixer, operation{kind: operationKindStop, slot: 0})
	rendered := pickLeft(renderBlocks(t, testMixer, 1))

	if rendered[0] < 0.99 {
		t.Fatalf("the stop cut to %v at the block start rather than beginning a ramp", rendered[0])
	}
	if last := rendered[len(rendered)-1]; last != 0 {
		t.Fatalf("the ramp ended on %v rather than on real silence", last)
	}
	if testMixer.voices[0].clip != nil {
		t.Fatal("the slot still plays after its ramp reached silence")
	}
	for index, sample := range renderBlocks(t, testMixer, 1) {
		if sample != 0 {
			t.Fatalf("the block after the ramp holds %v at %d, and the voice is gone", sample, index)
		}
	}
}

// A play and a stop in one flush arrive in one change list and are applied
// in order, so the voice is audible for zero samples. sfx still gives it a
// handle and an ending; what the mixer owes is silence.
func TestAPlayAndAStopInOneFlushsChangesAreAudibleForZeroSamples(t *testing.T) {
	testMixer := makeTestMixer(4)
	publishOperations(testMixer,
		makeStart(0, makeConstantClip(4*testBlock, 1, 1), makeMonoGains(1), false),
		operation{kind: operationKindStop, slot: 0},
	)

	for index, sample := range renderBlocks(t, testMixer, 1) {
		if sample != 0 {
			t.Fatalf("a voice played and stopped in one flush's changes was audible: %v at %d", sample, index)
		}
	}
}

// An update that reaches a slot a stop has already claimed is ignored, so a
// voice sfx has ended is never brought back mid-ramp.
func TestAnUpdateReachingAStoppedVoiceIsIgnored(t *testing.T) {
	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStart(0, makeConstantClip(4*testBlock, 1, 1), makeMonoGains(1), false))
	renderBlocks(t, testMixer, 1)

	publishOperations(testMixer,
		operation{kind: operationKindStop, slot: 0},
		makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(1), Rate: 1}),
	)
	rendered := pickLeft(renderBlocks(t, testMixer, 1))

	if last := rendered[len(rendered)-1]; last != 0 {
		t.Fatalf("the stopped voice ended its block on %v: the update after its stop brought it back", last)
	}
	if testMixer.voices[0].clip != nil {
		t.Fatal("the slot still plays: the update after its stop kept it")
	}
}

// A looping voice is gapless at its wrap: no gap, no repeated frame and none
// dropped. The ramp clip says which frame each sample came from, so the
// assertion is the whole sequence rather than an energy measure.
func TestALoopIsGaplessAtItsWrap(t *testing.T) {
	const frames = 300
	testMixer := makeTestMixer(4)
	clip := makeRampClip(frames)
	publishOperations(testMixer, makeStart(0, clip, makeMonoGains(1), true))

	for index, sample := range pickLeft(renderBlocks(t, testMixer, 1)) {
		if want := clip.samples[index%frames]; sample != want {
			t.Fatalf("frame %d of the loop is %v, want %v: the wrap repeated or dropped a frame", index, sample, want)
		}
	}
	if testMixer.voices[0].clip == nil {
		t.Fatal("the looping voice ended at the clip's end")
	}
}

// The wrap is frame-exact at a loop region's bounds: the frame after the
// region's last is its first, after an intro played once.
func TestAResidentLoopIsGaplessAtTheRegionsWrap(t *testing.T) {
	const frames, from, to = 300, 100, 250
	testMixer := makeTestMixer(4)
	clip := makeRampClip(frames)
	clip.loopRegion = m.Some(sfx.LoopRegion{Start: float32(from) / testRate, End: float32(to) / testRate})
	publishOperations(testMixer, makeStart(0, clip, makeMonoGains(1), true))

	for index, sample := range pickLeft(renderBlocks(t, testMixer, 1)) {
		frame := index
		if frame >= to {
			frame = from + (frame-to)%(to-from)
		}
		if want := clip.samples[frame]; sample != want {
			t.Fatalf("frame %d of the loop is %v, want frame %d's %v: the wrap repeated or dropped a frame", index, sample, frame, want)
		}
	}
	if testMixer.voices[0].clip == nil {
		t.Fatal("the looping voice ended at the loop region's end")
	}
}

// A loop region bounds a looping voice and nothing else. A one-shot on a clip
// with an intro, a loop and a tail plays all three and ends at the clip's end:
// the region says where to repeat between, never where the clip stops.
func TestAOneShotOnAClipWithALoopRegionPlaysToTheClipsEnd(t *testing.T) {
	const frames, from, to = testBlock / 2, 10, 20
	testMixer := makeTestMixer(4)
	clip := makeConstantClip(frames, 1, 1)
	clip.loopRegion = m.Some(sfx.LoopRegion{Start: float32(from) / testRate, End: float32(to) / testRate})
	publishOperations(testMixer, makeStart(0, clip, makeMonoGains(1), false))

	rendered := pickLeft(renderBlocks(t, testMixer, 1))
	for index := range frames {
		if rendered[index] != 1 {
			t.Fatalf("the one-shot went silent at frame %d, inside a clip it plays whole", index)
		}
	}
	for index := frames; index < len(rendered); index++ {
		if rendered[index] != 0 {
			t.Fatalf("the one-shot still sounded %d frames past its end", index-frames)
		}
	}
	if testMixer.voices[0].clip != nil {
		t.Fatal("the slot still plays after the clip ran out")
	}
}

// A one-shot ends at the clip's end rather than reading off the buffer. sfx
// computes the ending itself and emits a declicked stop; this is the
// backstop, for the mixer's frame-exact playhead getting there first.
func TestAOneShotEndsAtTheClipsEndRatherThanReadingPastIt(t *testing.T) {
	const frames = testBlock / 2
	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStart(0, makeConstantClip(frames, 1, 1), makeMonoGains(1), false))

	rendered := pickLeft(renderBlocks(t, testMixer, 1))
	for index := frames; index < len(rendered); index++ {
		if rendered[index] != 0 {
			t.Fatalf("the one-shot still sounded %d frames past its end", index-frames)
		}
	}
	if testMixer.voices[0].clip != nil {
		t.Fatal("the slot still plays after the clip ran out")
	}
}

// A voice starts at its start's offset, and one past the clip's end at its
// last frame rather than off its buffer.
func TestAVoiceStartsAtItsOffsetAndOnePastTheClipsEndAtItsLastFrame(t *testing.T) {
	const frames, offsetFrames = 4 * testBlock, testBlock / 2
	clip := makeRampClip(frames)
	for _, test := range []struct {
		offset time.Duration
		frame  int
	}{
		{time.Duration(offsetFrames) * time.Second / testRate, offsetFrames},
		{time.Second, frames - 1},
	} {
		testMixer := makeTestMixer(4)
		start := makeStart(0, clip, makeMonoGains(1), false)
		start.offset = test.offset
		publishOperations(testMixer, start)

		if first := pickLeft(renderBlocks(t, testMixer, 1))[0]; first != clip.samples[test.frame] {
			t.Fatalf("a voice started at %v began on %v, want frame %d's %v", test.offset, first, test.frame, clip.samples[test.frame])
		}
	}
}

// The device thread interpolates for a voice's rate and for nothing else:
// the base rate was converted once, in Prepare. What is left is the cheap
// interpolation, linear, landing where the arithmetic says.
func TestTheDeviceThreadInterpolatesForRate(t *testing.T) {
	const frames = 64
	testMixer := makeTestMixer(4)
	start := makeStart(0, makeRampClip(frames), makeMonoGains(1), false)
	start.params.Rate = 0.5
	publishOperations(testMixer, start)

	rendered := pickLeft(renderBlocks(t, testMixer, 1))
	for index := range 8 {
		want := float32(index) * 0.5 / frames
		if math.Abs(float64(rendered[index]-want)) > 1e-6 {
			t.Fatalf("at half rate frame %d is %v, want %v", index, rendered[index], want)
		}
	}
}

// A stereo voice is mixed through its whole 2×2 matrix, Gains[source][output],
// where a mono one uses row 0 alone.
func TestAStereoVoiceIsMixedThroughItsWholeGainMatrix(t *testing.T) {
	testMixer := makeTestMixer(4)
	clip := makeTestClip(2, slices.Repeat([]float32{0.5, 0.25}, 4*testBlock))
	publishOperations(testMixer, makeStart(0, clip, [2][2]float32{{0.2, 0.4}, {0.6, 0.8}}, false))

	rendered := renderBlocks(t, testMixer, 1)
	wantLeft := float32(0.5*0.2 + 0.25*0.6)
	wantRight := float32(0.5*0.4 + 0.25*0.8)
	if rendered[0] != wantLeft || rendered[1] != wantRight {
		t.Fatalf("the first frame is %v and %v, want %v and %v", rendered[0], rendered[1], wantLeft, wantRight)
	}
}

// A paused voice suspends rather than silences: its playhead stops, and it
// resumes on the sample it stopped on. It is declicked like everything else,
// so the block it pauses on ramps to silence rather than cutting.
func TestAPausedVoiceHoldsItsPlayheadAndRampsToSilence(t *testing.T) {
	const frames = 4 * testBlock
	testMixer := makeTestMixer(4)
	publishOperations(testMixer, makeStart(0, makeRampClip(frames), makeMonoGains(1), false))
	renderBlocks(t, testMixer, 1)
	pausedAt := testMixer.voices[0].playhead

	publishOperations(testMixer, makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(1), Rate: 1, Paused: true}))
	rendered := pickLeft(renderBlocks(t, testMixer, 1))

	if playhead := testMixer.voices[0].playhead; playhead != pausedAt {
		t.Fatalf("the paused playhead moved from %v to %v", pausedAt, playhead)
	}
	if last := rendered[len(rendered)-1]; last != 0 {
		t.Fatalf("the pause ended on %v rather than on silence", last)
	}
	renderBlocks(t, testMixer, 1)
	if testMixer.voices[0].clip == nil {
		t.Fatal("the paused voice ended, and a pause suspends rather than stops")
	}

	publishOperations(testMixer, makeUpdate(0, sfx.VoiceParams{Gains: makeMonoGains(1), Rate: 1}))
	renderBlocks(t, testMixer, 1)
	if playhead := testMixer.voices[0].playhead; playhead != pausedAt+testBlock {
		t.Fatalf("the resumed playhead is %v, want a block past the %v it paused on", playhead, pausedAt)
	}
}

// The whole machine, end to end: a game plays a real Ogg, sfx reads it, the
// prepare goroutine decodes it, Install mints its ID, the flush emits a
// start, the ring carries it across, and the mixer produces samples a device
// would be handed. Whether they sound right needs ears; that they are there,
// not silent, not clipping and in both channels is what makes the check by
// ear a check rather than a hunt for a build that produces nothing.
func TestAGamePlayingARealOggProducesSamplesForTheDevice(t *testing.T) {
	const path = "pianoroll.ogg"
	device := &fakeDevice{}
	h, plugin := spawnOverFake(t, device)
	h.Mount(path, readFixture(t))
	h.Record(func(queue sfx.Queue) { queue.Play(path, 0, sfx.VoiceOptions{}) })
	waitForReady(t, plugin)
	deadline := time.Now().Add(sfxtest.Timeout)
	for plugin.mixer.ring.published.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no flush emitted the voice's start in %v", sfxtest.Timeout)
		}
		h.Step()
		time.Sleep(time.Millisecond)
	}

	// A second of it, most of the fixture: the clip opens quietly, and its
	// first blocks would measure the piano roll's fade-in rather than the
	// mixer.
	var peak float32
	var leftEnergy, rightEnergy float64
	for index, sample := range renderBlocks(t, device.playing(), 100) {
		if sample > 1 || sample < -1 {
			t.Fatalf("sample %d is %v, outside what the device accepts", index, sample)
		}
		peak = max(peak, float32(math.Abs(float64(sample))))
		if index%outputChannels == 0 {
			leftEnergy += float64(sample) * float64(sample)
		} else {
			rightEnergy += float64(sample) * float64(sample)
		}
	}
	if peak < 0.01 {
		t.Fatalf("a second of a real Ogg peaked at %v, which is silence", peak)
	}
	if leftEnergy == 0 || rightEnergy == 0 {
		t.Fatalf("an output channel is empty: left %v, right %v", leftEnergy, rightEnergy)
	}
}
