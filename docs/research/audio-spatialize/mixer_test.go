package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// A click is a discontinuity, and a discontinuity is measurable. These render
// the mixer offline and look at the seam between blocks, so the listening test
// arrives already knowing what it should be able to hear.

import (
	"encoding/binary"
	"math"
	"testing"
)

// dcClip is a constant. It is not audio, and that is the point: with a source
// that never moves, the output IS the gain envelope, so a step in the envelope
// is a step in the output and nothing hides it. A 220 Hz sine moves 0.029 per
// sample all by itself, which buries the very thing being measured.
func dcClip(seconds float64, channels int) *PreparedClip {
	frames := int(seconds * deviceRate)
	s := make([]float32, frames*channels)
	for i := range s {
		s[i] = 1
	}
	return &PreparedClip{
		Name: "dc", Samples: s, Channels: channels, Frames: frames,
		SourceRate: deviceRate, Duration: seconds,
	}
}

// steadyClip is a sine at the device rate: smooth everywhere, so any step in
// the output is the mixer's doing and nothing else's.
func steadyClip(seconds float64, channels int) *PreparedClip {
	frames := int(seconds * deviceRate)
	s := make([]float32, frames*channels)
	for i := 0; i < frames; i++ {
		v := float32(math.Sin(2 * math.Pi * 220 * float64(i) / deviceRate))
		for c := 0; c < channels; c++ {
			s[i*channels+c] = v
		}
	}
	return &PreparedClip{
		Name: "sine", Samples: s, Channels: channels, Frames: frames,
		SourceRate: deviceRate, Duration: seconds,
	}
}

// renderOrbit drives the mixer the way the app does - a tick at 60 Hz writing a
// new matrix, a device block of 10 ms pulling - and returns the left channel.
func renderOrbit(mx *Mixer, clip *PreparedClip, blocks int, declick bool) []float32 {
	mx.SetDeclick(declick)
	mx.Start(0, clip, true, 0)
	mx.SetParams(0, slotParams{Matrix: panMatrix(-90, clip.Channels), Rate: 1})
	mx.Emit()

	const blockFrames = deviceRate * bufferMillis / 1000
	buf := make([]byte, blockFrames*4*deviceChans)
	out := make([]float32, 0, blocks*blockFrames)

	for b := 0; b < blocks; b++ {
		// One tick per block: at 10 ms blocks and a 60 Hz tick these interleave
		// roughly one to one, which is the case the design is built for.
		azimuth := float32(-90 + 180*float64(b)/float64(blocks))
		mx.SetParams(0, slotParams{Matrix: panMatrix(azimuth, clip.Channels), Rate: 1})
		mx.Emit()

		if _, err := mx.Read(buf); err != nil {
			panic(err)
		}
		for f := 0; f < blockFrames; f++ {
			out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8:])))
		}
	}
	return out
}

// maxStepAtBlockBoundaries is the click measure: how far the signal jumps
// between the last sample of one device block and the first of the next,
// against how far it moves inside a block.
func maxStepAtBlockBoundaries(out []float32) (boundary, interior float32) {
	const blockFrames = deviceRate * bufferMillis / 1000
	for i := 1; i < len(out); i++ {
		d := absf(out[i] - out[i-1])
		if i%blockFrames == 0 {
			if d > boundary {
				boundary = d
			}
		} else if d > interior {
			interior = d
		}
	}
	return boundary, interior
}

func TestDeclickingKeepsAPanFromSteppingAtEveryBlockBoundary(t *testing.T) {
	// A fast pan - the full 180 degrees in 20 blocks, 200 ms - because that is
	// what a source whipping past actually asks of the gain, and a slow one
	// would flatter the design.
	clip := dcClip(2, 1)

	on := renderOrbit(NewMixer(deviceRate, deviceChans), clip, 20, true)
	onBoundary, onInterior := maxStepAtBlockBoundaries(on)

	off := renderOrbit(NewMixer(deviceRate, deviceChans), clip, 20, false)
	offBoundary, offInterior := maxStepAtBlockBoundaries(off)

	t.Logf("declick on : boundary step %.6f, interior step %.6f", onBoundary, onInterior)
	t.Logf("declick off: boundary step %.6f, interior step %.6f", offBoundary, offInterior)

	// With the ramp, a block boundary is no more of an event than any other
	// sample: the gain moved a little on every one of them instead of all at
	// once on one of them.
	if onBoundary > onInterior*1.5 {
		t.Errorf("declicked, the boundary still steps: %.6f vs %.6f interior", onBoundary, onInterior)
	}
	// Without it, the boundary is where the whole tick's gain change lands.
	if offBoundary <= offInterior {
		t.Errorf("without declicking the boundary should be the worst step: %.6f vs %.6f",
			offBoundary, offInterior)
	}
	if offBoundary < onBoundary*2 {
		t.Errorf("declicking should make a large difference: on %.6f, off %.6f", onBoundary, offBoundary)
	}
}

func TestAFullRingLosesNoPlay(t *testing.T) {
	// #302's argument against gfx's latest-wins buffer, exercised: publish more
	// ticks than the ring can hold without ever reading, then drain.
	mx := NewMixer(deviceRate, deviceChans)
	clip := steadyClip(0.1, 1)
	const ticks = ringDepth * 3
	for i := 0; i < ticks; i++ {
		mx.Start(0, clip, false, 0)
		mx.Emit()
	}
	// Drain everything the ring holds, then keep draining as Emit makes room.
	buf := make([]byte, deviceRate*bufferMillis/1000*4*deviceChans)
	seen := 0
	for i := 0; i < ticks+ringDepth; i++ {
		before := mx.applied.Load()
		mx.Read(buf)
		seen += int(mx.applied.Load() - before)
		mx.Emit() // flush whatever merged into staging while the ring was full
	}
	if seen == 0 {
		t.Fatal("nothing was applied at all")
	}
	// The ops that could not be published merged into staging rather than being
	// dropped, so every Start eventually reaches the mixer.
	if len(mx.staging.ops) > 0 {
		t.Errorf("staging still holds %d ops after draining", len(mx.staging.ops))
	}
}

func TestTheDeviceThreadAllocatesNothing(t *testing.T) {
	// #302 states the rule; this is the rule being kept. A prototype that
	// allocated in Read would be measuring a mixer cog would never ship.
	mx := NewMixer(deviceRate, deviceChans)
	clip := steadyClip(1, 2)
	mx.Start(0, clip, true, 0)
	mx.SetParams(0, slotParams{Matrix: panMatrix(30, 2), Rate: 1.0})
	mx.Emit()

	buf := make([]byte, deviceRate*bufferMillis/1000*4*deviceChans)
	mx.Read(buf) // drain the start out of the way

	allocs := testing.AllocsPerRun(200, func() {
		mx.SetParams(0, slotParams{Matrix: panMatrix(30, 2), Rate: 1.0})
		mx.Emit()
		mx.Read(buf)
	})
	if allocs != 0 {
		t.Errorf("the tick-to-device path allocates %.1f times per block, want 0", allocs)
	}
}

func TestAStoppedVoiceRampsToSilenceRatherThanCutting(t *testing.T) {
	mx := NewMixer(deviceRate, deviceChans)
	clip := steadyClip(1, 1)
	mx.Start(0, clip, true, 0)
	mx.SetParams(0, slotParams{Matrix: panMatrix(0, 1), Rate: 1})
	mx.Emit()

	const blockFrames = deviceRate * bufferMillis / 1000
	buf := make([]byte, blockFrames*4*deviceChans)
	mx.Read(buf)

	mx.Stop(0)
	mx.Emit()
	mx.Read(buf)

	first := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:]))
	last := math.Float32frombits(binary.LittleEndian.Uint32(buf[(blockFrames-1)*8:]))
	if absf(last) > 1e-5 {
		t.Errorf("the stop should have reached silence by the end of its block: %.6f", last)
	}
	if absf(first) < 1e-6 {
		t.Errorf("the stop should not have cut instantly at the block's start: %.6f", first)
	}

	mx.Read(buf)
	for f := 0; f < blockFrames; f++ {
		if v := math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8:])); v != 0 {
			t.Fatalf("the block after a stop should be silent, frame %d is %.6f", f, v)
		}
	}
}
