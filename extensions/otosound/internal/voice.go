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
	// ring is the read-ahead a streamed voice reads instead of the Clip's
	// samples, and is nil for a resident one. It is made by the tick, at the
	// Voice's start, and the device thread reaches it through two atomic
	// counters and a fixed buffer - the same bargain the batch ring makes, for
	// the same reason.
	//
	// The voice holds the ring and not the stream that fills it. What the
	// device thread may touch is frames; the decoder, the goroutine and the
	// stopping of it are the tick's, and naming them here is how that would
	// stop being true.
	ring *pcmRing
	// pos is the playhead, in converted frames. It is fractional because Rate
	// is, and it is float64 because a five-minute track at 48kHz is fourteen
	// million frames, which float32 cannot count one at a time.
	//
	// For a resident voice it counts frames of the Clip; for a streamed one it
	// counts frames of the stream, which begins at the Voice's offset and runs
	// through the loop's wraps without ever going back - the read-ahead does
	// the wrapping, so the device thread's playhead only ever moves forwards.
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
	// starved is whether this frame found nothing in the read-ahead ring. A
	// starved voice contributes silence for its own slot and nothing else: one
	// Voice that could not keep up must never take the rest of the mix with it.
	starved bool
	// primed is whether the ring has ever had a frame for this voice. Until it
	// has, a starved voice holds its playhead instead of advancing it, which is
	// the Clip-load rule rather than the Device rule: the first fill is a
	// bounded hiccup the engine is actively fixing, so a Voice starts from its
	// offset with no catch-up. After it, an underrun advances - a playhead
	// moves whether or not anyone can hear it - so a stall is a gap in the
	// sound rather than a permanent drift behind the world.
	primed bool
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
	if v.ring != nil {
		return v.streamed(channel, stride)
	}
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

// streamed reads one channel of the frame under the playhead out of a
// read-ahead ring, interpolating for Rate exactly as a resident voice does.
// The frames in the ring were converted to the device rate by the read-ahead
// goroutine that decoded them, so this is the cheap interpolation and nothing
// else - a streamed Clip is no more work on the device thread than a resident
// one.
//
// A frame the read-ahead has not produced is silence for this slot. It is not
// silence for the block, not a wait, and not a decode: the whole reason the
// ring exists is that the only thing the device thread may do about a Voice
// that could not keep up is leave it out of this frame's sum.
func (v *voice) streamed(channel, stride int) float32 {
	held := v.ring.held()
	at := uint64(v.pos)
	if at >= held {
		v.starved = true
		return 0
	}
	v.starved, v.primed = false, true
	first := v.ring.at(at, channel, stride)
	frac := float32(v.pos - float64(at))
	if frac == 0 || at+1 >= held {
		// The frame after the last one held is not an underrun: it is the edge
		// of the read-ahead, and holding the sample for one frame is inaudible
		// where dropping to silence would be a click.
		return first
	}
	return first + (v.ring.at(at+1, channel, stride)-first)*frac
}

// holding is a streamed voice waiting for the head of its ring, which is the
// only case where the playhead stops for an underrun. See primed.
func (v *voice) holding() bool { return v.starved && !v.primed }

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
	if v.ring != nil {
		// The playhead is the read-ahead's permission to overwrite what is
		// behind it, and the length it publishes when it reaches the end of the
		// Clip is what ends a streamed one-shot. Until it does, the length
		// reads as unreachable and a stream simply carries on - which is what a
		// looping Voice is, and why it never ends by itself.
		v.ring.passed(uint64(v.pos))
		if v.pos >= float64(v.ring.length()) {
			v.active, v.clip, v.ring = false, nil, nil
		}
		return
	}
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
//
// A streamed voice always starts at zero, because its playhead counts frames of
// its own stream and the stream was opened at the offset: the seek happened on
// the read-ahead goroutine, where 460 us of decoder open costs nothing that can
// be heard.
func startFrame(o *op) float64 {
	if o.ring != nil {
		return 0
	}
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
