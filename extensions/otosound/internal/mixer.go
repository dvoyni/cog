//go:build !js

package internal

import (
	"encoding/binary"
	"math"
)

const (
	// outChannels is the Mixer's output width. It is stereo because panning is,
	// and it is a constant rather than a knob: a mono output makes
	// constant-power panning meaningless and a surround one is a different
	// Mixer.
	outChannels = 2
	// bytesPerFrame is one output frame as oto's FormatFloat32LE spells it.
	bytesPerFrame = outChannels * 4
)

// silent is the gain matrix a stopped or paused voice ramps towards.
var silent [2][2]float32

// mixer is everything the device thread runs. It is an io.Reader, which is the
// shape oto pulls through, and one Read is one block.
//
// It may touch its own voice table, the ring through atomics, and the prepared
// Clip data it was given. It may not allocate, take any lock, free anything,
// call into sound or kernel, or decode. Everything it needs was made once, at
// Voices(n) and in Prepare, which is what makes that rule keepable rather than
// merely stated.
//
// It never decides anything either. It holds no notion of a Bus, a priority, a
// position, the cap or a handle, and a stolen Voice reaches it as an ordinary
// stop.
type mixer struct {
	ring *ring
	// voices is the fixed table, MaxVoices long, indexed by VoiceSlot.
	voices []voice
	// live is this block's contributing slots, gathered once per block so the
	// per-frame loop carries no per-voice branching. Its capacity is MaxVoices
	// and it is never grown.
	live []int
}

func newMixer(handoff *ring, maxVoices int) *mixer {
	return &mixer{
		ring:   handoff,
		voices: make([]voice, maxVoices),
		live:   make([]int, 0, maxVoices),
	}
}

// Read produces one block. It drains every batch the tick has published, in
// order, applying each whole before a single sample of this block is made -
// which is why there is no tearing, only latency: a batch published mid-block
// is heard from the next block start, at most one block late.
func (mx *mixer) Read(buf []byte) (int, error) {
	frames := len(buf) / bytesPerFrame
	if frames == 0 {
		return 0, nil
	}
	if !mx.ring.pulled.Load() {
		mx.ring.pulled.Store(true)
	}
	mx.drain()
	mx.gather()

	// The ramp runs (f+1)/frames rather than f/frames so that the block ends
	// exactly on the target. That is what makes cur = target below true rather
	// than nearly true, and what lets a stop reach real silence instead of
	// something very close to it.
	inv := 1 / float32(frames)
	for f := range frames {
		t := float32(f+1) * inv
		var outL, outR float32
		for _, i := range mx.live {
			v := &mx.voices[i]
			if !v.active {
				continue
			}
			m00 := v.cur[0][0] + (v.target[0][0]-v.cur[0][0])*t
			m01 := v.cur[0][1] + (v.target[0][1]-v.cur[0][1])*t
			if v.clip.channels == 1 {
				s := v.sample(0, 1)
				outL += s * m00
				outR += s * m01
			} else {
				sl, sr := v.sample(0, 2), v.sample(1, 2)
				m10 := v.cur[1][0] + (v.target[1][0]-v.cur[1][0])*t
				m11 := v.cur[1][1] + (v.target[1][1]-v.cur[1][1])*t
				outL += sl*m00 + sr*m10
				outR += sl*m01 + sr*m11
			}
			if !v.paused {
				v.advance()
			}
		}
		at := f * bytesPerFrame
		binary.LittleEndian.PutUint32(buf[at:], math.Float32bits(clamp(outL)))
		binary.LittleEndian.PutUint32(buf[at+4:], math.Float32bits(clamp(outR)))
	}

	mx.settle()
	return frames * bytesPerFrame, nil
}

// drain applies every published batch, in order, at the block boundary. It then
// stores the applied counter, which is the tick's permission to free the Clips
// the batches below it destroyed.
//
// Several batches landing in one block are applied in order rather than
// coalesced: two ticks' operations taking effect at one instant is correct,
// because they were ordered and they stay ordered.
func (mx *mixer) drain() {
	write := mx.ring.write.Load()
	read := mx.ring.read.Load()
	if read == write {
		return
	}
	for ; read < write; read++ {
		mx.apply(mx.ring.slots[read%ringDepth])
	}
	mx.ring.read.Store(read)
}

// apply applies one batch whole, so a tick's operations become audible
// together: a Play then a SetVoice carrying a position, in one tick, is
// indistinguishable from a Play that carried the position.
func (mx *mixer) apply(b *batch) {
	for i := range b.ops {
		o := &b.ops[i]
		if o.kind == opDestroy {
			// Nothing. The stops ordered before this destroy already dropped
			// every reference the table held, and the tick frees behind the
			// applied counter. The Mixer never frees.
			continue
		}
		if int(o.slot) < 0 || int(o.slot) >= len(mx.voices) {
			continue
		}
		v := &mx.voices[o.slot]
		switch o.kind {
		case opStart:
			if o.clip == nil || o.clip.frames == 0 {
				continue
			}
			v.clip = o.clip
			v.loop = o.loop
			v.loopStart, v.loopEnd = o.clip.loopBounds()
			v.pos = startFrame(o)
			v.rate = rateOf(o.params.Rate)
			v.paused = o.params.Paused
			v.tgt = o.params.Gains
			// A start begins at its target rather than ramping up from silence:
			// a one-shot that faded in over a block would lose the transient
			// that is the reason it was played.
			v.cur = o.params.Gains
			v.active, v.fading, v.heard = true, false, false
		case opUpdate:
			if !v.active || v.fading {
				// A stop has already claimed this slot; an update must not undo
				// it, or a Voice sound has ended could be resurrected mid-ramp.
				continue
			}
			v.tgt = o.params.Gains
			v.rate = rateOf(o.params.Rate)
			v.paused = o.params.Paused
		case opStop:
			if !v.active {
				continue
			}
			v.fading = true
			if !v.heard {
				v.cur = silent
			}
		}
	}
}

// gather collects the slots that will contribute to this block and fixes each
// one's target, so the per-frame loop carries no per-voice decisions. A paused
// voice that has already ramped to silence is left out entirely: it is frozen
// and inaudible, and multiplying its frame by zero would say the same thing
// more slowly.
func (mx *mixer) gather() {
	mx.live = mx.live[:0]
	for i := range mx.voices {
		v := &mx.voices[i]
		if !v.active {
			continue
		}
		v.target = v.tgt
		if v.fading || v.paused {
			v.target = silent
		}
		if v.paused && v.cur == v.target {
			continue
		}
		mx.live = append(mx.live, i)
	}
}

// settle closes the block: every ramp lands exactly on its target, and a voice
// that was fading is freed now that it has reached silence.
func (mx *mixer) settle() {
	for i := range mx.voices {
		v := &mx.voices[i]
		if !v.active {
			v.clip, v.cur = nil, silent
			continue
		}
		v.cur = v.target
		v.heard = true
		if v.fading {
			v.active, v.fading = false, false
			v.clip, v.cur = nil, silent
		}
	}
}

// clamp holds the output inside what the device accepts. A mix of many Voices
// can exceed unity, and a wrapped sample is far louder than a clipped one.
func clamp(v float32) float32 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}
