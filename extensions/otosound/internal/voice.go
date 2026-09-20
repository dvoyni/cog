//go:build !js

package internal

// voice is one slot of the Mixer's table. The table is a fixed array indexed
// directly by sound.VoiceSlot, which is what "an Adapter allocates nothing per
// play" means in practice, and it carries no generation because sound
// guarantees a slot is stopped before it reuses one - a guarantee an Adapter
// may rely on rather than defend against.
type voice struct {
	// clip is the resident Clip this voice reads, and is nil when the slot is
	// free. It is set by the tick, through a batch, and never looked up: the
	// clip table is a map the tick writes, and a map the device thread read
	// would be exactly the shared mutable state this design exists to avoid.
	clip *clipData
	// pos is the playhead, in converted frames. It is fractional because Rate
	// is, and it is float64 because a five-minute track at 48kHz is fourteen
	// million frames, which float32 cannot count one at a time.
	pos float64
	// loopStart and loopEnd bound the span a looping voice repeats between,
	// resolved from the Clip's Loop Region once, when the voice starts, so that
	// no block ever touches a Maybe or a seconds-to-frames conversion.
	loopStart, loopEnd float64
	// cur is the gain matrix the last block ended on and target is the one this
	// block ramps to. The ramp between them is the declick.
	cur, target [2][2]float32
	// tgt is the matrix the tick last asked for, which is target except when a
	// stop or a pause is overriding it with silence.
	tgt  [2][2]float32
	rate float32
	// active is whether the slot is sounding at all.
	active bool
	// fading is a stop in progress: ramp to silence over this block, then free
	// the slot. It is why VoiceEndedEvent precedes the silence by a few
	// milliseconds, which sound states as contract.
	fading bool
	paused bool
	loop   bool
	// heard is whether this voice has ever produced a block. A stop that
	// reaches a voice which never has takes its gain to silence outright rather
	// than ramping, so a play and a stop in one tick is audible for zero
	// samples - which is what the seam promises.
	heard bool
}

// sample reads one channel of the frame under the playhead, interpolating
// linearly for a fractional Rate. This is the cheap interpolation the device
// thread is allowed; the base-rate conversion that would alias was done once,
// in Prepare.
//
// The frame after the last one of a looping span is the first one of that span
// rather than the one after it in the buffer, which is what makes a loop
// gapless: no repeated frame at the wrap, and none dropped.
func (v *voice) sample(channel, stride int) float32 {
	i := int(v.pos)
	first := v.clip.samples[i*stride+channel]
	frac := float32(v.pos - float64(i))
	if frac == 0 {
		return first
	}
	next := i + 1
	if float64(next) >= v.loopEnd {
		if v.loop {
			next = int(v.loopStart)
		} else {
			next = i
		}
	}
	second := v.clip.samples[next*stride+channel]
	return first + (second-first)*frac
}

// advance moves the playhead on by one output frame, wrapping a looping voice
// at its span's end and ending a one-shot at the Clip's.
//
// A one-shot that runs out here is the backstop rather than the ordinary path:
// sound computes the ending from Duration and emits a Stop, which is declicked.
// This is what happens when the Mixer's sample-exact playhead reaches the end
// first, and it cuts rather than ramps - the samples to ramp with are precisely
// what has run out.
func (v *voice) advance() {
	v.pos += float64(v.rate)
	if v.pos < v.loopEnd {
		return
	}
	if !v.loop {
		v.active, v.clip = false, nil
		return
	}
	span := v.loopEnd - v.loopStart
	for v.pos >= v.loopEnd {
		v.pos -= span
	}
	if v.pos < v.loopStart {
		v.pos = v.loopStart
	}
}

// startFrame is where a VoiceStart's Offset lands in converted frames, clamped
// into the Clip so that an offset past the end starts at the last frame rather
// than indexing off the buffer.
func startFrame(o *op) float64 {
	at := o.offset.Seconds() * float64(o.clip.rate)
	if at < 0 {
		return 0
	}
	if last := float64(o.clip.frames) - 1; at > last {
		return last
	}
	return at
}

// rateOf clamps a playback rate into what a playhead can move by. A negative
// rate would run a Voice backwards off the front of its buffer, and sound never
// sends one; zero is a legitimate freeze.
func rateOf(rate float32) float32 {
	if rate < 0 {
		return 0
	}
	return rate
}
