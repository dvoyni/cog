//go:build !js

package internal

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/dvoyni/cog/slots/sound"
)

// The Mixer's tests render to a buffer and never to a device. Every property
// below is assertable without a sound card, which is what keeps them runnable
// in CI - and they are in this package rather than in a shared suite because
// what they check is a property of code that no test above the Port can see.
//
// There is no race detector in this environment, so the tests that touch the
// ring from two goroutines run with -count=10 instead and say so.

const (
	// testRate and testBlock are the defaults: 48 kHz and a 10 ms block, which
	// is also the declick ramp's length, because the ramp is one block.
	testRate  = defaultSampleRate
	testBlock = testRate / 100
)

// constantClip is a clip whose every sample is the same value. With a source
// that never moves, the output is the gain envelope, which is the only way to
// measure a ramp without the waveform burying it: a 220 Hz sine moves 0.029 per
// sample on its own.
func constantClip(frames, channels int, value float32) *clipData {
	samples := make([]float32, frames*channels)
	for i := range samples {
		samples[i] = value
	}
	return &clipData{
		samples:    samples,
		frames:     frames,
		channels:   channels,
		rate:       testRate,
		sourceRate: testRate,
		duration:   float32(frames) / testRate,
	}
}

// rampClip is a mono clip whose frames count upwards inside [0, 1), so that a
// rendered sample says which frame it came from exactly.
func rampClip(frames int) *clipData {
	samples := make([]float32, frames)
	for i := range samples {
		samples[i] = float32(i) / float32(frames)
	}
	return &clipData{
		samples:    samples,
		frames:     frames,
		channels:   1,
		rate:       testRate,
		sourceRate: testRate,
		duration:   float32(frames) / testRate,
	}
}

// mono is the gain matrix sound computes for a non-positional mono Voice: one
// row, both outputs, scaled by the volume.
func mono(volume float32) [2][2]float32 {
	var gains [2][2]float32
	gains[0][0], gains[0][1] = volume, volume
	return gains
}

func newTestMixer(maxVoices int) (*mixer, *ring) {
	handoff := newRing(maxVoices)
	return newMixer(handoff, maxVoices), handoff
}

func start(handoff *ring, slot sound.VoiceSlot, clip *clipData, gains [2][2]float32, loop bool) {
	handoff.record(op{
		kind:   opStart,
		slot:   slot,
		clip:   clip,
		loop:   loop,
		params: sound.VoiceParams{Gains: gains, Rate: 1},
	}, false)
	handoff.publish()
}

// render pulls whole blocks and decodes them back into interleaved float32, so
// a test reads what a device would have been handed.
func render(t *testing.T, mx *mixer, blocks int) []float32 {
	t.Helper()
	buf := make([]byte, testBlock*bytesPerFrame)
	out := make([]float32, 0, blocks*testBlock*outChannels)
	for range blocks {
		n, err := mx.Read(buf)
		if err != nil {
			t.Fatalf("the Mixer failed a Read: %v", err)
		}
		if n != len(buf) {
			t.Fatalf("the Mixer produced %d bytes for a %d byte block", n, len(buf))
		}
		for at := 0; at < n; at += 4 {
			out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(buf[at:])))
		}
	}
	return out
}

// left picks one output channel out of an interleaved render.
func left(out []float32) []float32 {
	picked := make([]float32, len(out)/outChannels)
	for i := range picked {
		picked[i] = out[i*outChannels]
	}
	return picked
}

// This is the obligation most likely to fail quietly, so it is asserted first
// and over a full block: the device thread may not allocate, and every buffer
// it needs - the ring, its batches, the voice table, the live list - was made
// once at Voices(n).
//
// oto's own pull path allocates about 114 times a second, which is the figure
// this is measured against rather than included in: what the Adapter owes is
// that its contribution is zero.
func TestTheDeviceThreadAllocatesNothingOverAFullBlock(t *testing.T) {
	mx, handoff := newTestMixer(8)
	clip := constantClip(4096, 2, 0.5)
	start(handoff, 0, clip, mono(0.8), true)
	buf := make([]byte, testBlock*bytesPerFrame)

	allocations := testing.AllocsPerRun(200, func() {
		// A publish and a drain every block, so the measurement covers the
		// handoff and not only the arithmetic.
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
		t.Fatalf("a block allocated %v times, and the device thread may not allocate at all", allocations)
	}
}

// sound emits target values once per flush and the Adapter ramps to them across
// its own blocks, because declicking is a function of the output block rate and
// only the Adapter knows it.
//
// Measured in the prototype: without the ramp the gain steps 0.078 of amplitude
// in a single sample at every block boundary; with it, 0.00016 - smaller than
// the ramp's own per-sample motion, and a ~480x difference repeating at 100 Hz,
// which is what a zipper is.
func TestAGainChangeRampsAcrossTheBlockRatherThanSteppingAtIt(t *testing.T) {
	mx, handoff := newTestMixer(4)
	const quiet, loud = float32(0.1), float32(0.9)
	start(handoff, 0, constantClip(4*testBlock, 1, 1), mono(quiet), false)
	render(t, mx, 1)

	handoff.record(op{kind: opUpdate, slot: 0, params: sound.VoiceParams{Gains: mono(loud), Rate: 1}}, false)
	handoff.publish()
	out := left(render(t, mx, 1))

	step := float64(loud-quiet) / testBlock
	atBoundary := math.Abs(float64(out[0] - quiet))
	if atBoundary > step*1.001 {
		t.Fatalf("the gain stepped %v across the block boundary, and the ramp's own step is %v", atBoundary, step)
	}
	if jump := float64(loud - quiet); atBoundary > jump/100 {
		t.Fatalf("the gain stepped %v across the boundary, which is not a ramp of the %v it had to cover", atBoundary, jump)
	}
	for i := 1; i < len(out); i++ {
		if inside := math.Abs(float64(out[i] - out[i-1])); inside > step*1.001 {
			t.Fatalf("the gain stepped %v inside the block at sample %d, and the ramp's own step is %v", inside, i, step)
		}
	}
	if got := out[len(out)-1]; math.Abs(float64(got-loud)) > 1e-6 {
		t.Fatalf("the block ended on %v rather than exactly on its target %v", got, loud)
	}
}

// A Stop is the same mechanism as any other parameter change: ramp to zero over
// the block, and free the slot only once it has reached silence. This is why
// VoiceEndedEvent precedes the silence by a few milliseconds, which sound
// states as contract.
func TestAStoppedVoiceRampsToSilenceBeforeItsSlotIsFreed(t *testing.T) {
	mx, handoff := newTestMixer(4)
	start(handoff, 0, constantClip(4*testBlock, 1, 1), mono(1), false)
	render(t, mx, 1)

	handoff.record(op{kind: opStop, slot: 0}, false)
	handoff.publish()
	out := left(render(t, mx, 1))

	if out[0] < 0.9 {
		t.Fatalf("the stop cut to %v at the block start rather than beginning a ramp", out[0])
	}
	if last := out[len(out)-1]; math.Abs(float64(last)) > 1e-6 {
		t.Fatalf("the ramp ended on %v rather than on real silence", last)
	}
	if mx.voices[0].active {
		t.Fatal("the slot was still active after its ramp reached silence")
	}
	for i, sample := range render(t, mx, 1) {
		if sample != 0 {
			t.Fatalf("the block after the ramp holds %v at %d, and the voice is gone", sample, i)
		}
	}
}

// A play and a stop in one tick arrive in one batch and are applied in order,
// so the Voice is audible for zero samples. It still gets a handle and a
// VoiceEndedEvent, which are sound's to give; what the Mixer owes is silence.
func TestAPlayAndAStopInOneBatchIsAudibleForZeroSamples(t *testing.T) {
	mx, handoff := newTestMixer(4)
	handoff.record(op{
		kind:   opStart,
		slot:   0,
		clip:   constantClip(4*testBlock, 1, 1),
		params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.record(op{kind: opStop, slot: 0}, false)
	handoff.publish()

	for i, sample := range render(t, mx, 1) {
		if sample != 0 {
			t.Fatalf("a voice played and stopped in one batch was audible: %v at %d", sample, i)
		}
	}
}

// A looping Voice is gapless at its loop point: no gap, no repeated frame and
// none dropped. The ramp clip says which frame each sample came from, so the
// assertion is the whole sequence rather than an energy measure.
func TestALoopIsGaplessAtItsWrap(t *testing.T) {
	const frames = 300
	mx, handoff := newTestMixer(4)
	clip := rampClip(frames)
	start(handoff, 0, clip, mono(1), true)

	out := left(render(t, mx, 1))
	for i, got := range out {
		if want := clip.samples[i%frames]; got != want {
			t.Fatalf("frame %d of the loop is %v, want %v - the wrap repeated or dropped a frame", i, got, want)
		}
	}
	if !mx.voices[0].active {
		t.Fatal("the looping voice ended at the Clip's end")
	}
}

// A one-shot ends at the Clip's end rather than reading off the buffer. sound
// computes the ending itself and emits a declicked Stop; this is the backstop
// for the Mixer's sample-exact playhead getting there first.
func TestAOneShotEndsAtTheClipsEndRatherThanReadingPastIt(t *testing.T) {
	mx, handoff := newTestMixer(4)
	const frames = testBlock / 2
	start(handoff, 0, constantClip(frames, 1, 1), mono(1), false)

	out := left(render(t, mx, 1))
	for i := frames; i < len(out); i++ {
		if out[i] != 0 {
			t.Fatalf("the one-shot was still sounding %d frames past its end", i-frames)
		}
	}
	if mx.voices[0].active {
		t.Fatal("the slot was still active after the Clip ran out")
	}
}

// The device thread interpolates for Rate and for nothing else. The base rate
// was converted once, in Prepare, which is what obligation 3 asks for; what is
// left here is the cheap interpolation, and this is that it is linear and lands
// where the arithmetic says.
func TestTheDeviceThreadInterpolatesForRate(t *testing.T) {
	const frames = 64
	mx, handoff := newTestMixer(4)
	clip := rampClip(frames)
	handoff.record(op{
		kind:   opStart,
		slot:   0,
		clip:   clip,
		params: sound.VoiceParams{Gains: mono(1), Rate: 0.5},
	}, false)
	handoff.publish()

	out := left(render(t, mx, 1))
	for i := range 8 {
		want := float32(i) * 0.5 / float32(frames)
		if got := out[i]; math.Abs(float64(got-want)) > 1e-6 {
			t.Fatalf("at half rate frame %d is %v, want %v", i, got, want)
		}
	}
}

// A paused Voice suspends rather than silences: the playhead stops and resumes
// on the same sample. It is declicked like everything else, so the block it is
// paused on ramps to silence rather than cutting.
func TestAPausedVoiceHoldsItsPlayheadAndRampsToSilence(t *testing.T) {
	const frames = 4 * testBlock
	mx, handoff := newTestMixer(4)
	start(handoff, 0, rampClip(frames), mono(1), false)
	render(t, mx, 1)
	at := mx.voices[0].pos

	handoff.record(op{
		kind:   opUpdate,
		slot:   0,
		params: sound.VoiceParams{Gains: mono(1), Rate: 1, Paused: true},
	}, false)
	handoff.publish()
	out := left(render(t, mx, 1))

	if mx.voices[0].pos != at {
		t.Fatalf("the paused playhead moved from %v to %v", at, mx.voices[0].pos)
	}
	if last := out[len(out)-1]; math.Abs(float64(last)) > 1e-6 {
		t.Fatalf("the pause ended on %v rather than on silence", last)
	}
	render(t, mx, 1)
	if !mx.voices[0].active {
		t.Fatal("the paused voice ended, and a pause suspends rather than stops")
	}
}
