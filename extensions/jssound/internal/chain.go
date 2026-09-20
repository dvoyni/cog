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

	// ctxRate is the context's sample rate, which a streamed Voice's chunk
	// buffers are made at and which its output frames are counted in.
	ctxRate int
	// stream is a streamed Voice's decode-ahead, and is nil for a resident one -
	// which is the only thing in this file that asks which tier a Clip is in.
	stream *streamer
	// placed is every chunk this Voice has scheduled and that has not certainly
	// finished, each with the node it was started through. A chunk is kept after
	// it is scheduled because a rate change re-places it: every `when` already
	// on the clock was computed at the old rate, so they are all recomputed from
	// the frame numbers, which do not move.
	placed []placed
	// streamBase is the output frame the anchor at sits on. It is zero for a run
	// that has just started and moves whenever the anchor does, so the mapping
	// from an output frame to a context time is one subtraction and one divide
	// wherever it is needed.
	streamBase int64
	// late counts chunks the clock had gone entirely past by the time the flush
	// reached them, which is this tier's underrun: audio that was decoded and
	// will never be heard. It is counted rather than hidden, and a test asserts
	// it stays zero through an ordinary run.
	late int
}

// placed is one scheduled chunk: the buffer and frame numbers it was made from,
// and the source node carrying it.
type placed struct {
	chunk chunk
	node  js.Value
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
		ctxRate:      audio.sampleRate(),
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
	c.offset, c.at = offset, now
	if c.clip.streams() {
		// A streamed Voice has no buffer to start and no loopStart to set. It
		// gets a decode-ahead seeked to the same position, and its first chunk
		// is scheduled by the next pump - which is the read-ahead prime the spec
		// states for a streamed Voice on the other Adapter too, a tick of
		// silence rather than a tick of nothing.
		c.streamBase = 0
		c.stream = newStreamer(c.audio, c.clip, c.ctxRate, c.clip.sourceFrame(offset), c.loop)
		return
	}
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
}

// pump keeps a streamed Voice's schedule full. It runs once per flush, from
// Emit, with the same context clock every other operation in that batch uses.
//
// It is the whole of the tier's scheduling: tell the decode-ahead where the
// playhead has reached, take what it has finished, and put each chunk on the
// clock back to back. Nothing here decodes, so a flush costs a streamed Voice a
// couple of node constructions and no samples.
func (c *chain) pump(now float64) {
	if c.stream == nil || c.paused {
		return
	}
	c.stream.advance(c.outAt(now))
	for _, one := range c.stream.take() {
		c.place(one, now)
	}
	c.prune(now)
}

// outAt is where the playhead is in this run's own output frames, at the context
// rate. It is the same derivation position() makes in Clip seconds, against the
// same anchor, which is why a streamed Voice needs no second playhead: the
// chunks sit on the context clock, so the clock is the playhead.
func (c *chain) outAt(now float64) int64 {
	if c.paused || c.rate <= 0 || now <= c.at {
		return c.streamBase
	}
	return c.streamBase + int64((now-c.at)*float64(c.rate)*float64(c.ctxRate))
}

// whenOf is the context time an output frame is heard at, under the anchor and
// rate in force now.
func (c *chain) whenOf(frame int64) float64 {
	rate := float64(c.rate)
	if rate <= 0 {
		rate = 1
	}
	return c.at + float64(frame-c.streamBase)/(rate*float64(c.ctxRate))
}

// place schedules one chunk and records it. This is the whole of "back to back
// on the context clock": the chunk's `when` comes from its frame number, so two
// consecutive chunks meet at a frame boundary in the context's own timeline and
// the browser is never asked to join them.
//
// A `when` that has already passed is the one failure this has. Web Audio starts
// such a source immediately, which would put the chunk's head on top of the tail
// of the one before it - a doubled fragment at a seam, which is exactly the
// repeated frame the promise forbids. So a late chunk is started at now with an
// offset into itself instead, which drops the part that should already have been
// heard and joins the rest in the right place, and the fact is counted.
//
// It is also how a run's first chunk lands: the decode-ahead primes a tick after
// the Play, so chunk zero is a little late by construction, and dropping that
// much of its head is the streamed tier's version of the silence otosound's
// Mixer plays while a ring fills.
func (c *chain) place(one chunk, now float64) {
	when := c.whenOf(one.at)
	offset := 0.0
	if when < now {
		offset = (now - when) * float64(c.rate)
		when = now
		if offset >= float64(one.frames)/float64(c.ctxRate) {
			// Entirely in the past. Dropping it is the honest answer and the
			// Device's own rule one level down: a playhead advances whether or
			// not anyone could hear it.
			c.late++
			return
		}
	}
	node := c.audio.newSource(one.buffer)
	node.Get("playbackRate").Set("value", float64(c.rate))
	node.Call("connect", c.splitter)
	node.Call("start", when, offset)
	c.placed = append(c.placed, placed{chunk: one, node: node})
}

// prune lets go of chunks the clock has gone past. The node has finished on its
// own, nothing Go holds points at it, and syscall/js drops the JS-side reference
// when the Go value is collected - so a Voice playing a five-minute track keeps
// a second of buffers rather than five minutes of them.
func (c *chain) prune(now float64) {
	kept := c.placed[:0]
	for _, one := range c.placed {
		if c.whenOf(one.chunk.at+int64(one.chunk.frames)) > now {
			kept = append(kept, one)
		}
	}
	clear(c.placed[len(kept):])
	c.placed = kept
}

// replace puts every chunk the clock has not gone past back on the clock under
// the anchor and rate in force now, and drops the rest.
//
// Every `when` already scheduled was computed from the anchor and the rate they
// were scheduled under, so a rate change cannot just set playbackRate the way
// the resident tier does: the chunks after the one in flight would play at the
// new speed from the old times and every seam after it would open. What this
// does instead is stop them and put the same buffers back from their frame
// numbers, which do not move - the one in flight resumed at the sample it was
// cut on, through start(when, offset). So a rate change, and a resume, are a
// re-placement rather than a restart, and the decoder never moves.
func (c *chain) replace(now float64) {
	passed := c.outAt(now)
	kept := make([]chunk, 0, len(c.placed))
	for _, one := range c.placed {
		c.stopNode(one.node, now)
		if one.chunk.at+int64(one.chunk.frames) > passed {
			kept = append(kept, one.chunk)
		}
	}
	clear(c.placed)
	c.placed = c.placed[:0]
	for _, one := range kept {
		c.place(one, now)
	}
}

// freeze reads both playheads against the anchor in force and then moves the
// anchor to now. The two are read before either is written, because the Clip
// position and the output frame are the same instant said twice.
func (c *chain) freeze(now float64) {
	position, out := c.position(now), c.outAt(now)
	c.offset, c.at, c.streamBase = position, now, out
}

// silenceStream stops every scheduled chunk without letting go of the buffer
// behind it, which is what a pause needs: the samples are still the right
// samples, so a resume puts them back on the clock rather than seeking a
// decoder and re-deciding what the Voice was about to play.
func (c *chain) silenceStream(when float64) {
	for i := range c.placed {
		c.stopNode(c.placed[i].node, when)
		c.placed[i].node = js.Undefined()
	}
}

// stopStream ends a streamed Voice: the decode-ahead is halted, every chunk
// already on the clock is stopped, and the buffers are let go.
//
// Halting here is the spec's "a streamed Voice's read-ahead stops with it",
// and it holds however the Voice ended - finished, stolen, released or cut by a
// despawn - because all four reach this Adapter as the same stop.
func (c *chain) stopStream(when float64) {
	if c.stream != nil {
		c.stream.halt()
	}
	for _, one := range c.placed {
		c.stopNode(one.node, when)
	}
	clear(c.placed)
	c.placed = c.placed[:0]
}

// stopNode schedules one chunk's source to stop. A node that has already ended
// on its own refuses a second stop on some implementations, which is the Voice
// ending twice and is harmless.
func (c *chain) stopNode(node js.Value, when float64) {
	if !node.Truthy() {
		return
	}
	defer func() { _ = recover() }()
	node.Call("stop", when)
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
	if c.paused || c.rate <= 0 || now <= c.at {
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
		// counted at the rate they were played at - and, for a streamed Voice,
		// so is the output frame the clock has reached.
		c.freeze(now)
		c.rate = rate
		if c.stream != nil {
			c.replace(now)
		} else if c.source.Truthy() {
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
	c.freeze(now)
	c.paused = true
	c.silence(now)
	c.stopSource(now + float64(declickTails)*c.timeConstant)
	c.silenceStream(now + float64(declickTails)*c.timeConstant)
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
	if c.stream != nil {
		// The decode-ahead kept its place through the pause and the chunks it
		// had already finished are still the right samples, so the anchor moves
		// to now and the same buffers go back on the clock. A streamed resume is
		// therefore sample-continuous and costs no decoder seek, which is the one
		// thing this tier does better than the resident one.
		c.at = now
		c.replace(now)
		return
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
		c.stopStream(now)
		return
	}
	c.silence(now)
	c.stopSource(now + float64(declickTails)*c.timeConstant)
	c.stopStream(now + float64(declickTails)*c.timeConstant)
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
	if c.stream != nil {
		c.stream.halt()
	}
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
