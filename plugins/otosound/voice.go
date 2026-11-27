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
	// clip is the resident clip the voice reads, nil while the slot holds no
	// voice. It was resolved on the flush side when the start was staged, so
	// the device thread never reads the clip table.
	clip *preparedClip
	// playhead is the frame of the clip's samples under the voice, fractional
	// because a rate is, and a float64 because a five-minute track at 48 kHz
	// is fourteen million frames, which a float32 cannot count one at a time.
	playhead float64
	// loopStart and loopEnd bound the span a looping voice repeats between, in
	// the frames the clip's samples hold, resolved once at its start so no
	// block touches a conversion from seconds. A one-shot's span is the whole
	// clip, whose end is where it stops.
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
}

// start begins the voice that started names, in place of whatever the slot
// held, at its target gain rather than ramping up from silence: a one-shot
// that faded in over a block would lose the transient it was played for. Its
// playhead starts at the start's offset, an offset past the clip's end
// starting at its last frame. A start on a clip the flush side could not
// resolve plays nothing.
func (v *voice) start(started *operation) {
	clip := started.clip
	if clip == nil {
		return
	}
	heldFrames := float64(len(clip.samples) / clip.channels)
	*v = voice{
		clip:        clip,
		playhead:    max(0, min(heldFrames-1, clip.heldFrameAt(started.offset.Seconds()))),
		loopEnd:     heldFrames,
		startGains:  started.params.Gains,
		targetGains: started.params.Gains,
		rate:        max(0, float64(started.params.Rate)),
		paused:      started.params.Paused,
		loop:        started.loop,
	}
	// A loop region bounds a looping voice and nothing else: a one-shot on a
	// clip with an intro, a loop and a tail plays all three and ends at the
	// clip's end.
	region, declared := clip.loopRegion.Get()
	if !started.loop || !declared {
		return
	}
	loopStart := math.Round(clip.heldFrameAt(float64(region.Start)))
	loopEnd := min(heldFrames, math.Round(clip.heldFrameAt(float64(region.End))))
	// A region sfx accepted spans at least one of its source's frames, which
	// rounds to none only where the clip's samples are held at a far lower
	// rate; such a span would never let the playhead wrap, so it loops whole.
	if loopStart >= 0 && loopStart < loopEnd {
		v.loopStart = loopStart
		v.loopEnd = loopEnd
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
// for a voice's rate, the base rate having been converted once, in Prepare.
// The frame after the last of a looping span is the span's first, which is
// what makes the loop gapless: no frame repeated at the wrap, and none
// dropped.
func (v *voice) sampleFrame() (left, right float32) {
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
	firstLeft := v.clip.samples[frame*channels]
	left = firstLeft + (v.clip.samples[next*channels]-firstLeft)*fraction
	if channels == 1 {
		return left, left
	}
	firstRight := v.clip.samples[frame*channels+1]
	right = firstRight + (v.clip.samples[next*channels+1]-firstRight)*fraction
	return left, right
}

// advance moves the playhead on by the voice's rate, wrapping a looping voice
// at its span's end and ending a one-shot at the clip's. A one-shot that runs
// out here is the backstop rather than the ordinary path: sfx computes the
// ending from the clip's duration and emits a stop, which is declicked. Here
// the playhead reached the end first, and the voice is cut, since the samples
// to ramp with are what ran out.
func (v *voice) advance() {
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

// endBlock lands the voice's ramp on its target and marks it heard, or, when
// it was stopping, frees its slot, dropping its clip. A slot holding no voice
// is left as it is, since the next start replaces the whole of it.
func (v *voice) endBlock() {
	switch {
	case v.stopping:
		*v = voice{}
	case v.clip != nil:
		v.startGains = v.endGains
		v.heard = true
	}
}
