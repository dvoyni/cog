//go:build !js

package otosound

import (
	"math"

	"github.com/dvoyni/cog/plugins/sfx"
)

// voice is one slot of the mixer's voice table, which only the device thread
// touches: the flush side reaches it through the handoff ring's operations
// alone.
type voice struct {
	// clip is the clip the voice plays, nil while the slot holds no voice. It
	// was resolved on the flush side when the start was staged, so the device
	// thread never reads the clip table.
	clip *preparedClip
	// ring is the read-ahead ring a streamed voice reads in place of its
	// clip's samples, nil for a resident one. The voice holds the ring and
	// never the read-ahead filling it: the decoder, its goroutine and its
	// halting are the flush side's.
	ring *readAheadRing
	// playhead is the frame under the voice, fractional because a rate is,
	// and a float64 because a five-minute track at 48 kHz is fourteen million
	// frames, which a float32 cannot count one at a time. A resident voice's
	// counts the clip's samples; a streamed voice's counts its stream's
	// frames, from its offset on through every wrap, since its read-ahead
	// wraps, so it only ever moves forwards.
	playhead float64
	// loopStart and loopEnd bound the span a looping resident voice repeats
	// between, in the frames the clip's samples hold, resolved once at its
	// start so no block touches a conversion from seconds. A one-shot's span
	// is the whole clip, whose end is where it stops.
	loopStart, loopEnd float64
	// startGains is the matrix the last block ended on, which this block ramps
	// from, and endGains the one it ramps to: targetGains, or silence for a
	// stopping or paused voice. The ramp between them is the declick.
	startGains, endGains [2][2]float32
	// targetGains is the matrix sfx last asked for.
	targetGains [2][2]float32
	// rate is how many of the clip's frames the playhead moves by a frame of
	// output, never negative.
	rate float64
	// stopping is a stop in progress: the voice ramps to silence over this
	// block, and its slot is freed at the block's end.
	stopping bool
	paused   bool
	loop     bool
	// heard is whether a block has played the voice. A stop that reaches a
	// voice no block has played takes it to silence outright, so a play and a
	// stop in one flush's changes are audible for zero samples.
	heard bool
	// primed is whether a streamed voice's ring has ever had a frame for it.
	// Until it has, the voice holds its playhead, since the first fill is a
	// clip load, which starts from its offset with no catch-up; once it has,
	// an underrun advances it, since a playhead advances whether or not
	// anyone can hear it.
	primed bool
	// producedFrames and streamLength are a streamed voice's ring's counts,
	// read once a block, so no frame of the block touches an atomic: the
	// frames its read-ahead had produced, and its stream's length, unknown
	// until the read-ahead reaches the clip's end.
	producedFrames uint64
	streamLength   float64
}

// start begins the voice that started names, in place of whatever the slot
// held, at its target gain rather than ramping up from silence: a one-shot
// that faded in over a block would lose the transient it was played for. A
// resident voice's playhead starts at the start's offset, in frames at
// outputSampleRate, the rate its clip's samples are held at, an offset past
// the clip's end starting at its last frame; a streamed voice's starts at
// zero, since its read-ahead was opened at the offset. A start on a clip the
// flush side could not resolve, or on a streamed clip given no ring, plays
// nothing.
func (v *voice) start(started *operation, outputSampleRate int) {
	clip := started.clip
	if clip == nil || clip.streams() && started.ring == nil {
		return
	}
	rate := float64(outputSampleRate)
	heldFrames := float64(len(clip.samples) / clip.channels)
	*v = voice{
		clip:        clip,
		ring:        started.ring,
		loopEnd:     heldFrames,
		startGains:  started.params.Gains,
		targetGains: started.params.Gains,
		rate:        max(0, float64(started.params.Rate)),
		paused:      started.params.Paused,
		loop:        started.loop,
	}
	if v.ring != nil {
		// The read-ahead wraps a looping streamed voice, in its source's
		// frames, so the mixer holds no loop bounds for it.
		return
	}
	v.playhead = max(0, min(heldFrames-1, started.offset.Seconds()*rate))
	// A loop region bounds a looping voice and nothing else: a one-shot on a
	// clip with an intro, a loop and a tail plays all three and ends at the
	// clip's end.
	if started.loop {
		v.loopStart, v.loopEnd = clip.loopBounds(rate, heldFrames)
	}
}

// update restates the voice's target, rate and pause, which the next block
// ramps to. An update reaching a slot a stop has already claimed is ignored,
// so a voice sfx has ended is never brought back mid-ramp.
func (v *voice) update(params sfx.VoiceParams) {
	if v.clip == nil || v.stopping {
		return
	}
	v.targetGains = params.Gains
	v.rate = max(0, float64(params.Rate))
	v.paused = params.Paused
}

// stop ramps the voice to silence over the next block, which frees its slot
// at its end; a voice no block has played goes to silence outright.
func (v *voice) stop() {
	if v.clip == nil {
		return
	}
	v.stopping = true
	if !v.heard {
		v.startGains = silentGains
	}
}

// beginBlock reads a streamed voice's ring's counts once for the block, so
// a frame the read-ahead produces mid-block is heard from the next.
func (v *voice) beginBlock() {
	if v.ring == nil {
		return
	}
	v.producedFrames = v.ring.producedFrames()
	v.streamLength = float64(v.ring.streamLength())
}

// rampGains is the matrix at rampFraction, the fraction of this block's ramp
// from startGains to endGains that a frame has reached.
func (v *voice) rampGains(rampFraction float32) [2][2]float32 {
	var gains [2][2]float32
	for source := range gains {
		for output := range gains[source] {
			gains[source][output] = v.startGains[source][output] + (v.endGains[source][output]-v.startGains[source][output])*rampFraction
		}
	}
	return gains
}

// sampleFrame reads the frame under the playhead, its left and right samples,
// a mono clip's one sample answered as both, interpolating linearly between
// it and the next frame for a fractional playhead: the cheap interpolation
// for a voice's rate, the base rate having been converted once, in Prepare or
// in the read-ahead. The frame after the last of a looping span is the span's
// first, which is what makes the loop gapless: no frame repeated at the wrap,
// and none dropped.
func (v *voice) sampleFrame() (left, right float32) {
	if v.ring != nil {
		return v.sampleStreamedFrame()
	}
	channels := v.clip.channels
	frame := int(v.playhead)
	next := frame + 1
	if float64(next) >= v.loopEnd {
		if v.loop {
			next = int(v.loopStart)
		} else {
			next = frame
		}
	}
	fraction := float32(v.playhead - float64(frame))
	return interpolateFrame(v.clip.samples, frame*channels, next*channels, channels, fraction)
}

// sampleStreamedFrame reads the frame under a streamed voice's playhead out
// of its read-ahead ring, interpolating for its rate as a resident voice
// does. A frame the read-ahead has not produced is an underrun, silence for
// this slot and not for the block: one voice that could not keep up must
// never take the rest of the mix with it. The frame just past the last one
// produced is the edge of the read-ahead, not an underrun, and holds its
// sample for that frame, which is inaudible where dropping to silence would
// click.
func (v *voice) sampleStreamedFrame() (left, right float32) {
	frame := uint64(v.playhead)
	if frame >= v.producedFrames {
		return 0, 0
	}
	v.primed = true
	nextFrame := frame + 1
	if nextFrame >= v.producedFrames {
		nextFrame = frame
	}
	fraction := float32(v.playhead - float64(frame))
	return interpolateFrame(v.ring.samples, v.ring.sampleIndex(frame), v.ring.sampleIndex(nextFrame), v.ring.channels, fraction)
}

// advance moves the playhead on by the voice's rate, wrapping a looping voice
// at its span's end and ending a one-shot at the clip's. A one-shot that runs
// out here is the backstop rather than the ordinary path: sfx computes the
// ending from the clip's duration and emits a stop, which is declicked. Here
// the playhead reached the end first, and the voice is cut, since the samples
// to ramp with are what ran out.
func (v *voice) advance() {
	if v.ring != nil {
		v.advanceStream()
		return
	}
	v.playhead += v.rate
	if v.playhead < v.loopEnd {
		return
	}
	if !v.loop {
		v.clip = nil
		return
	}
	v.playhead = v.loopStart + math.Mod(v.playhead-v.loopStart, v.loopEnd-v.loopStart)
}

// advanceStream moves a streamed voice's playhead on, unless it waits for
// its ring's first frames. The voice ends where its playhead reaches the
// length its read-ahead published at the clip's end; until one is
// published, the stream simply carries on, which is what a looping voice is.
func (v *voice) advanceStream() {
	if v.primed {
		v.playhead += v.rate
	}
	if v.playhead >= v.streamLength {
		v.clip = nil
		v.ring = nil
	}
}

// endBlock records a streamed voice's playhead as passed, which lets its
// read-ahead overwrite what is behind it, once a block. It then lands the
// voice's ramp on its target and marks it heard, or, when it was stopping,
// frees its slot, dropping its clip and its ring. A slot holding no voice is
// left as it is, since the next start replaces the whole of it.
func (v *voice) endBlock() {
	if v.ring != nil {
		v.ring.passed.Store(uint64(v.playhead))
	}
	switch {
	case v.stopping:
		*v = voice{}
	case v.clip != nil:
		v.startGains = v.endGains
		v.heard = true
	}
}

// interpolateFrame is the frame at index in samples, interleaved, channels
// to a frame, blended linearly towards the one at nextIndex by fraction: its
// left and right samples, a mono frame's one sample answered as both. It is
// the one interpolation the device thread does, for a voice's rate, whichever
// tier holds the frames.
func interpolateFrame(samples []float32, index, nextIndex, channels int, fraction float32) (left, right float32) {
	firstLeft := samples[index]
	left = firstLeft + (samples[nextIndex]-firstLeft)*fraction
	if channels == 1 {
		return left, left
	}
	firstRight := samples[index+1]
	right = firstRight + (samples[nextIndex+1]-firstRight)*fraction
	return left, right
}
