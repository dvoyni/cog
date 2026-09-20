//go:build js

package internal

import (
	"math"
	"syscall/js"

	"github.com/dvoyni/cog/slots/sound"
)

// declickTails is how many time constants a ramp to silence is given before the
// source behind it is stopped. setTargetAtTime approaches its target
// exponentially, so five of them is 0.7% of the gain it started from - below
// what a step of that size would have to be to be heard as a click.
const declickTails = 5

// chain is one playing Voice as a Web Audio graph:
//
//	source -> splitter -> gains[src][out] -> merger -> destination
//
// The four gains are sound's 2x2 matrix, Gains[src][out], and nothing else. That
// is the whole reason there is no PannerNode here: the W3C equations are
// transcribed once, above the seam, so a Voice heard through this Adapter and
// the same Voice heard through otosound cannot disagree about where it is.
//
// A mono Clip drives the same four. The splitter's channel interpretation is
// discrete by the specification, so a one-channel source up-mixes with silence
// in channel 1, and row 1 of the matrix - which sound leaves zero for a mono
// Clip - multiplies that silence. One graph shape for both widths, and no branch
// on a Clip's channel count anywhere below the seam.
//
// A chain is made per start rather than per slot, because Web Audio gives no way
// to restart an AudioBufferSourceNode: a start, and a Seek - which is a start
// with an offset - is always a fresh node. The gains are fresh with it, which is
// what lets a stolen Voice ramp to silence on its own gains while the Voice that
// took its slot plays at full gain through its own.
type chain struct {
	audio    *webAudio
	source   js.Value
	splitter js.Value
	merger   js.Value
	gains    [2][2]js.Value

	clip *clipData
	// gain is the matrix last asked for, kept so that a resume can restore what
	// a pause ramped away.
	gain   [2][2]float32
	rate   float32
	loop   bool
	paused bool

	// offset and at are the playhead, kept as a pair: the Clip position this run
	// of the source began at, and the context time it began at. Everything about
	// the playhead is derived from them, because Web Audio has no playhead to
	// read - a source node reports neither where it is nor that it has moved.
	offset float64
	at     float64
	// loopStart and loopEnd bound the span a looping Voice repeats between, in
	// seconds, resolved from the Clip's Loop Region once at the start.
	loopStart, loopEnd float64
	// timeConstant is the browser's own render quantum in seconds, which is what
	// every ramp here is shaped over.
	timeConstant float64
}

// startChain builds a Voice's graph and starts it at now, offset seconds into
// the Clip. Nothing is scheduled ahead: now is the context clock read once for
// the whole batch, so every operation one tick states lands at one instant.
func startChain(audio *webAudio, clip *clipData, start *sound.VoiceStart, now, timeConstant float64) *chain {
	offset := start.Offset.Seconds()
	if offset < 0 {
		offset = 0
	}
	if limit := float64(clip.duration); offset > limit {
		offset = limit
	}
	c := &chain{
		audio:        audio,
		clip:         clip,
		gain:         start.Params.Gains,
		rate:         rateOf(start.Params.Rate),
		loop:         start.Loop,
		paused:       start.Params.Paused,
		offset:       offset,
		at:           now,
		timeConstant: timeConstant,
	}
	c.loopStart, c.loopEnd = clip.loopSpan()

	c.splitter = audio.newSplitter()
	c.merger = audio.newMerger()
	for src := range outChannels {
		for out := range outChannels {
			gain := audio.newGain()
			// A start begins at its target rather than fading in: a one-shot
			// that faded in over a block would lose the transient that is the
			// reason it was played. This is otosound's rule, kept here so that
			// the same Play sounds the same on both.
			setGainNow(gain, c.gain[src][out], now)
			c.splitter.Call("connect", gain, src, 0)
			gain.Call("connect", c.merger, 0, out)
			c.gains[src][out] = gain
		}
	}
	c.merger.Call("connect", audio.destination)

	// A Voice that starts paused is a graph with no source: Paused stops the
	// source rather than zeroing its gain, because a resident buffer started
	// with start() keeps advancing whatever its gain is and a resume would land
	// wherever the wall clock had reached.
	if !c.paused {
		c.play(now, offset)
	}
	return c
}

// play attaches a fresh source node at a Clip position and starts it. It is the
// only place start(when, offset) is called, and it is called for a Play, for a
// Seek and for a resume alike - which is what makes a Seek sample-accurate here
// for the same reason it is on the other two Adapters: the offset is a position
// in the Clip and never a block boundary.
func (c *chain) play(now, offset float64) {
	source := c.audio.newSource(c.clip.buffer)
	source.Get("playbackRate").Set("value", float64(c.rate))
	if c.loop {
		source.Set("loop", true)
		source.Set("loopStart", c.loopStart)
		source.Set("loopEnd", c.loopEnd)
	}
	source.Call("connect", c.splitter)
	source.Call("start", now, offset)
	c.source = source
	c.offset, c.at = offset, now
}

// position is where the playhead is now, in Clip seconds. It exists for one
// caller - a pause, which has to remember where to come back to - and it is
// derived rather than read because Web Audio has nothing to read: a source node
// reports neither its position nor that it has one.
//
// A looping Voice wraps inside its Loop Region, which is the same wrap sound
// performs above the seam over the same span, so the two agree about where a
// paused looping ambience resumes.
func (c *chain) position(now float64) float64 {
	if c.paused || c.rate <= 0 {
		return c.offset
	}
	at := c.offset + (now-c.at)*float64(c.rate)
	if c.loop {
		if span := c.loopEnd - c.loopStart; span > 0 && at >= c.loopEnd {
			return c.loopStart + math.Mod(at-c.loopStart, span)
		}
		return at
	}
	if limit := float64(c.clip.duration); at > limit {
		return limit
	}
	return at
}

// update restates the Voice's parameters. The gains ramp, the rate does not, and
// a change in Paused starts or stops the source.
//
// The rate is set outright because a step in playbackRate is a step in the
// derivative and never in the amplitude: it is heard as a pitch change, which is
// what it is, and not as the click declicking exists to remove. otosound sets it
// the same way, in the same place, for the same reason - and ramping it would
// make the position above an integral of a curve rather than a sum, which is
// what a pause would then have to resume from.
func (c *chain) update(params sound.VoiceParams, now float64) {
	if rate := rateOf(params.Rate); rate != c.rate {
		// Re-anchored before the rate moves, so the seconds already played are
		// counted at the rate they were played at.
		c.offset, c.at = c.position(now), now
		c.rate = rate
		if c.source.Truthy() {
			c.source.Get("playbackRate").Set("value", float64(rate))
		}
	}
	switch {
	case params.Paused && !c.paused:
		c.pause(now)
	case !params.Paused && c.paused:
		c.gain = params.Gains
		c.resume(now)
		return
	}
	c.gain = params.Gains
	if c.paused {
		return
	}
	for src := range outChannels {
		for out := range outChannels {
			setGain(c.gains[src][out], c.gain[src][out], now, c.timeConstant)
		}
	}
}

// pause suspends the Voice on the sample it is on. The gains ramp away first and
// the source is stopped behind them, so a pause is declicked exactly as a stop
// is; the position is taken before either, which is what makes the resume land
// where the pause did.
func (c *chain) pause(now float64) {
	c.offset, c.at = c.position(now), now
	c.paused = true
	c.silence(now)
	c.stopSource(now + float64(declickTails)*c.timeConstant)
}

// resume starts a fresh source at the position the pause left, and fades the
// gains back in over one quantum.
//
// It fades rather than restoring outright because a resume lands mid-waveform,
// where a step from silence is the discontinuity a click is made of. That is the
// opposite of a fresh start, which begins at silence in the Clip itself and has
// a transient worth keeping.
func (c *chain) resume(now float64) {
	c.paused = false
	for src := range outChannels {
		for out := range outChannels {
			setGainNow(c.gains[src][out], 0, now)
			setGain(c.gains[src][out], c.gain[src][out], now, c.timeConstant)
		}
	}
	c.play(now, c.offset)
}

// stop ends the Voice: the gains ramp to silence and the source is stopped once
// they have arrived. That is why sound.VoiceEndedEvent precedes the silence by a
// few milliseconds, which sound.md states as contract.
//
// A Voice stopped at the instant it started is cut outright instead. A play and
// a stop in one tick is audible for zero samples, which is what the seam
// promises, and a ramp there would be a Voice the game never asked to hear.
func (c *chain) stop(now float64) {
	if now <= c.at {
		c.silenceNow(now)
		c.stopSource(now)
		return
	}
	c.silence(now)
	c.stopSource(now + float64(declickTails)*c.timeConstant)
}

// silence ramps every gain to zero over the browser's quantum.
func (c *chain) silence(now float64) {
	for src := range outChannels {
		for out := range outChannels {
			setGain(c.gains[src][out], 0, now, c.timeConstant)
		}
	}
}

// silenceNow takes every gain to zero with no ramp at all.
func (c *chain) silenceNow(now float64) {
	for src := range outChannels {
		for out := range outChannels {
			setGainNow(c.gains[src][out], 0, now)
		}
	}
}

// stopSource schedules the source to stop and lets go of it. The node itself
// lives on the JS side until it has stopped, which is what keeps the AudioBuffer
// behind it alive across a Destroy in the same batch: the stops are applied
// first, the browser holds the buffer for as long as a source is still playing
// it, and the Clip's memory comes back when both have finished.
func (c *chain) stopSource(when float64) {
	if !c.source.Truthy() {
		return
	}
	func() {
		// A source that has already ended on its own - a one-shot whose Clip ran
		// out before sound's playhead said so - refuses a second stop on some
		// implementations. That is the Voice ending twice, which is harmless.
		defer func() { _ = recover() }()
		c.source.Call("stop", when)
	}()
	c.source = js.Undefined()
}

// disconnect takes the graph off the destination. It is called on a chain whose
// ramp has certainly finished, and on every chain when the Engine stops; a
// js.Value needs nothing else, because syscall/js drops the JS-side reference
// when the Go value is collected.
func (c *chain) disconnect() {
	defer func() { _ = recover() }()
	c.merger.Call("disconnect")
}

// rateOf clamps a playback rate into what a playhead can move by. A negative
// rate would run a Voice backwards, which Web Audio permits and sound never
// sends; zero is a legitimate freeze.
func rateOf(rate float32) float32 {
	if rate < 0 {
		return 0
	}
	return rate
}
