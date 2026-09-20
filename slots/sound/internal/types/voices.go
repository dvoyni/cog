package types

import (
	"iter"
	"time"
)

// VoiceInfo is one live Voice as the read-only view shows it: what the game
// commanded, and what the engine derived from it.
//
// It carries no gain matrix and no pan. Panning is engine arithmetic, pinned
// once by sound's own package tests against W3C's published numbers, not
// re-asserted by every game that makes a noise.
type VoiceInfo struct {
	// Voice is the handle the Play handed back.
	Voice Voice
	// Clip is what this Voice is playing. Compare it with Equal.
	Clip ClipRef
	// Bus is the group this Voice's volume is folded into.
	Bus Bus
	// Playhead is where the Voice is, in seconds. It is tick-accurate and not
	// sample-accurate: sound computes it from the Clip's duration and the
	// Voice's rate, the same way it computes endings, and the Device is
	// somewhere inside the current block when anyone reads it.
	Playhead float32
	// Duration is the Clip's length in seconds, and is zero while the Voice is
	// still waiting on its Clip.
	Duration float32
	// Params is what the Voice is set to now.
	Params Params
	// Paused is whether this Voice is suspended, so a reader does not have to
	// resolve it: its own Params.Paused, or an engine Pause, which suspends
	// every Voice at once. What the game itself asked for stays readable on
	// Params, so the two are never confused - but a reader asking whether this
	// playhead is moving is answered about the playhead and not about who
	// stopped it.
	Paused bool
	// Audibility is the final computed gain before panning - volume x bus x
	// falloff x cone. It is the scalar stealing ranks on, and it is what a
	// game's test asserts: that the alarm is playing, on the right Bus, audible
	// at 0.4 rather than 0.
	//
	// It is never max(L, R) taken off the gain matrix. A source orbiting the
	// Listener at a constant radius does not move it at all, while every entry
	// of the matrix moves the whole way; reading it off the matrix would make
	// "quietest" and "furthest away" two policies that disagree with each other
	// on a circle. The matrix is not here at all, because panning is engine
	// arithmetic pinned once by sound's own tests against W3C's numbers.
	Audibility float32
	// Distance is how far the Voice is from the Listener, in the game's own
	// units, as of the last flush. It is 0 on a non-positional Voice, which has
	// no position to be far from anything.
	Distance float32
}

// slotState is what one entry of the fixed table currently is.
type slotState uint8

const (
	// slotEmpty is a slot no Voice holds. Its index is on the minter's free
	// list, or is about to be.
	slotEmpty slotState = iota
	// slotLive is a slot holding a Voice, whether or not that Voice is audible.
	slotLive
	// slotEnded is a slot whose Voice ended in the flush that is running. It
	// survives until the batch is collected, so the Adapter gets its stop, and
	// is cleared at the end of the flush.
	slotEnded
)

// voiceSlot is one entry of the fixed table. The handle is the index, so this
// struct holds no index of its own; what it holds of the handle is the whole
// handle, which is what makes the staleness test one compare.
type voiceSlot struct {
	state slotState
	voice Voice
	clip  ClipRef
	bus   Bus
	// busGain is the volume of the Voice's Bus as of the last fold. It is held
	// here rather than looked up so that one pass both computes the gain and
	// notices that it moved, which is the whole of what re-emitting the Voices
	// on a Bus costs. It is zero on a slot the fold has not reached yet - every
	// slot in the flush that started its Voice, and that Voice is owed a
	// VoiceStart in that same flush anyway.
	busGain float32
	// spatialGain is falloff x cone as of the last spatialize pass, and is 1
	// on a non-positional Voice. It is held beside busGain for the same reason
	// busGain is: one pass both computes the gain and notices that it moved,
	// which is what re-emits a Voice the Listener walked away from without any
	// operation naming it. It is zero on a slot the pass has not reached yet.
	spatialGain float32
	// azimuth and elevation are the source's bearing about the Listener, in
	// degrees, as of the last spatialize pass. They are retained rather than
	// recomputed in collect, so that "did the pan move" is one compare.
	//
	// They are not on VoiceInfo. The view carries audibility and distance and
	// not the pan: a game's test asserts what it commanded and what the engine
	// derived from it, and a game checking its sound is on the right side of
	// the player checks where it put the source, which is its own data.
	azimuth, elevation float32
	// distance is how far the Voice is from the Listener, in the game's units.
	distance float32
	// params is the Voice's current Params, last value winning.
	params Params
	// offset is where in the Clip the Voice begins, in seconds. It is kept
	// because a Voice waiting on its Clip starts from its offset with no
	// catch-up: catch-up would serve music sync and ruin one-shots.
	offset float64
	// playhead and duration are seconds. They are float64 because a playhead is
	// accumulated a tick at a time over minutes, and the view reports float32.
	playhead float64
	duration float64
	// loopFrom and loopTo are the span a looping Voice repeats between, in
	// seconds. They are resolved from the Clip's Loop Region once, when the
	// Voice binds, so that a tick's wrap is two compares and never a Maybe -
	// and they are the whole Clip on a Clip that declares no region, which is
	// what an absent region means and what every untagged Clip says.
	loopFrom, loopTo float64
	channels         int
	clipID           ClipID
	// born is when this Voice started, counted in Voices rather than measured,
	// and it is what "ties broken by age, oldest first" reads. A counter rather
	// than a playhead or a tick number because two Voices played in one tick at
	// one position tie on every other term exactly - a double footstep, two
	// shell casings, a burst weapon - and a tick number would leave that tie
	// to be broken by whatever order the table happened to be walked in.
	born uint64
	// pending is a Voice whose Clip is not resident yet: addressable, Stop-able
	// and silent, its playhead not advancing until the Clip installs.
	pending bool
	// fresh is a Voice created in the flush that is running. It is what makes
	// ReasonFailed arrive one or more ticks after the Play and never in the
	// same tick.
	fresh bool
	// started is a Voice bound to its Clip in this flush and owed a VoiceStart.
	started bool
	// changed is a Voice whose Params moved in this flush and is owed a
	// VoiceUpdate.
	changed bool
	// emitted is a Voice the Adapter holds on this slot, so a stop is owed when
	// it ends. A Voice that was played and ended inside one flush never became
	// audible and is emitted neither way.
	emitted bool
}

// info renders the slot as the view shows it.
func (s *voiceSlot) info(enginePaused bool) VoiceInfo {
	return VoiceInfo{
		Voice:      s.voice,
		Clip:       s.clip,
		Bus:        s.bus,
		Playhead:   float32(s.playhead),
		Duration:   float32(s.duration),
		Params:     s.params,
		Paused:     s.suspended(enginePaused),
		Audibility: s.audibility(),
		Distance:   s.distance,
	}
}

// looping is whether this Voice repeats rather than ending. It says that the
// Voice repeats and never where: where is the Clip's to declare, and with no
// Loop Region on it the Voice repeats the whole of it.
func (s *voiceSlot) looping() bool { return s.params.Loop.Or(false) }

// loopStart and loopEnd are the span a looping Voice repeats between, in
// seconds. They are the Clip's Loop Region where it declares one and the whole
// Clip where it does not, which is what an absent region means - so a Clip with
// no tags loops exactly as every Clip looped before the tags existed.
//
// A loop point is a fact about a Clip and never a parameter of a Voice: nothing
// a game says reaches these, and Loop stays the bool it always was. It simply
// stops meaning repeat the whole Clip and starts meaning repeat the way this
// Clip says to.
func (s *voiceSlot) loopStart() float64 { return s.loopFrom }

func (s *voiceSlot) loopEnd() float64 { return s.loopTo }

// rate is the playback rate this Voice advances at, 1 being the Clip's own. A
// negative Pitch would run a Voice backwards off the front of its buffer and
// sound never sends one; zero is a legitimate freeze.
func (s *voiceSlot) rate() float32 {
	rate := s.params.Pitch.Or(1)
	if rate < 0 {
		return 0
	}
	return rate
}

// suspended is whether this Voice's playhead is stopped: the game paused it, or
// the engine did. An engine Pause suspends every Voice, the playhead rule
// included, and it beats a Device that is not ready - nothing advances.
func (s *voiceSlot) suspended(enginePaused bool) bool {
	return enginePaused || s.params.Paused.Or(false)
}

// wrap brings a looping Voice's playhead back inside its span. It loops rather
// than clamping because a looping Voice never ends by itself, and it subtracts
// in a loop rather than taking a remainder so that a rate that overshoots by
// several spans in one tick still lands inside one.
//
// The span it wraps at is the Clip's loop end and not its duration. On a Clip
// with an intro and a loop those are different places, and a playhead that
// wrapped at the duration would report the Voice playing through a tail the
// Adapter stopped playing a bar ago.
func (s *voiceSlot) wrap() {
	if s.playhead < s.loopEnd() {
		// Short of the loop end is either the intro or the loop itself, and
		// neither is a wrap. The intro is the whole point: a looping Voice
		// starts at the head of the Clip and runs into its loop, so a playhead
		// before the loop start is carried forwards into it and never lifted
		// up to it.
		return
	}
	span := s.loopEnd() - s.loopStart()
	if span <= 0 {
		s.playhead = s.loopStart()
		return
	}
	// Subtracting lands inside the span by construction: the playhead is at
	// least the loop end, so taking whole spans off it cannot fall below the
	// loop start. It is the same arithmetic the Mixer's own playhead runs, and
	// the two must agree to the frame or the view describes a different sound
	// from the one in the room.
	for s.playhead >= s.loopEnd() {
		s.playhead -= span
	}
}

// audibility is the Voice's final gain before panning: its own volume folded
// with its Bus's, with its falloff and with its cone. It is one scalar rather
// than anything read off the gain matrix, which is what makes "quietest" and
// "furthest from the Listener" one policy rather than two, and what gives a
// non-positional Voice a rank without a special rule.
//
// Panning is on the far side of this: the matrix is this scalar spread across
// two ears, so an orbit at a constant radius moves every entry of the matrix
// and does not move this at all.
func (s *voiceSlot) audibility() float32 {
	return s.params.Volume.Or(1) * s.busGain * s.spatialGain
}

// rank is what stealing orders this Voice by. It reads audibility as it stands,
// which is the audibility a paused Voice would have if it were not paused:
// pausing neither protects a Voice nor puts it first on the block, and ranking
// a suspended Voice at zero would make "pause the music for a cutscene" a
// reliable way to lose the music.
func (s *voiceSlot) rank() rank {
	return rank{priority: s.params.Priority.Or(0), audibility: s.audibility(), born: s.born}
}

// spatialize recomputes what the Listener makes of this Voice: its distance,
// its bearing, and the falloff and cone that multiply into its audibility. It
// reports whether any of that moved, which is what re-emits a Voice nothing
// named this tick.
//
// A Voice with no position is non-positional: no falloff, no cone, and a
// bearing of 0, which is "heard centred" stated as a number rather than as a
// special case in the panner. The first position a Voice receives makes it
// positional for the rest of its life, and Params is where that fact is kept -
// a Maybe that has been set cannot be unset by a merge, so there is nothing
// else to remember.
func (s *voiceSlot) spatialize(listener *Listener) bool {
	spatial, azimuth, elevation, distance := float32(1), float32(0), float32(0), float32(0)
	if position, ok := s.params.Position.Get(); ok {
		offset := position.Sub(listener.position)
		distance = offset.Length()
		spatial = s.params.spatialGain(listener.position, distance)

		bearing, height := azimuthElevation(position, listener.position, listener.front, listener.up)
		azimuth, elevation = float32(bearing), float32(height)
	}

	if spatial == s.spatialGain && azimuth == s.azimuth && elevation == s.elevation && distance == s.distance {
		return false
	}
	s.spatialGain, s.azimuth, s.elevation, s.distance = spatial, azimuth, elevation, distance
	return true
}

// voiceParams is what the slot looks like below the seam: the W3C equalpower
// matrix for the Voice's bearing, scaled by the one scalar that says how loud
// it is.
//
// A non-positional Voice runs the same equations at a bearing of 0, because
// "heard centred" is a position on the circle and not a second rule - which is
// what makes a mono Clip 0.707 in each ear rather than unity in both, and leaves
// a stereo Clip on its own diagonal.
//
// The Bus is already inside that scalar and appears nowhere below: an Adapter
// does not know Buses exist, and there is no second place a volume is decided.
func (s *voiceSlot) voiceParams(enginePaused bool) VoiceParams {
	gains := equalPowerGains(float64(s.azimuth), s.channels >= 2)
	volume := s.audibility()
	for source := range gains {
		for output := range gains[source] {
			gains[source][output] *= volume
		}
	}
	return VoiceParams{Gains: gains, Rate: s.rate(), Paused: s.suspended(enginePaused)}
}

// Voices is sound's live Voice view: a fixed table of slots, written once a
// tick by the flush and read by anyone holding a read lock on it.
//
// It is a resource and not a command. gfx.FrameSnapshot is a command because a
// frame is not retained and there is nothing to read between ticks; Voices are
// retained, so a command would be a second door onto a fact already sitting
// still.
type Voices struct {
	slots []voiceSlot
	live  int
	// enginePaused is an engine Pause standing over the whole table. It is one
	// flag rather than a field per slot because an engine Pause is not a
	// property of a Voice: a Voice played under one is suspended from its first
	// tick without anybody having said so about it.
	enginePaused bool
	// sequence stamps each Voice with the order it was started in, which is
	// what "ties broken by age" compares. It counts starts rather than ticks,
	// so two Voices played in one tick are still ordered.
	sequence uint64
	// vacated is the slots a start took over from a Voice the Adapter was
	// holding, so the stop it is owed survives the slot being overwritten in
	// the same flush. It is what "sound stops a slot before it reuses one"
	// costs when a steal and its replacement land in one tick, and it keeps
	// its capacity between ticks.
	vacated []VoiceSlot
}

// NewVoices builds the table at its fixed size. The size is sound's cap, and
// the Adapter is told it once before any Emit.
func NewVoices(maxVoices int) *Voices {
	return &Voices{slots: make([]voiceSlot, maxVoices)}
}

// Len reports how many Voices are live.
func (v *Voices) Len() int { return v.live }

// Info reports one Voice, and whether it exists. A handle whose generation does
// not match its slot's addresses nothing, so a game holding a handle across a
// steal, a stop and a recycle of that index never reads the Voice that took the
// slot.
func (v *Voices) Info(voice Voice) (VoiceInfo, bool) {
	slot := v.slotOf(voice)
	if slot == nil {
		return VoiceInfo{}, false
	}
	return slot.info(v.enginePaused), true
}

// All yields every live Voice, in slot order.
func (v *Voices) All() iter.Seq[VoiceInfo] {
	return func(yield func(VoiceInfo) bool) {
		for i := range v.slots {
			if v.slots[i].state != slotLive {
				continue
			}
			if !yield(v.slots[i].info(v.enginePaused)) {
				return
			}
		}
	}
}

// slotOf resolves a handle to its slot, or nil when the handle addresses
// nothing: the zero handle, a handle no slot can hold because its play found
// none, or a handle whose generation its slot has moved past.
func (v *Voices) slotOf(voice Voice) *voiceSlot {
	index := int(voice.idx())
	if voice == NoVoice || index >= len(v.slots) {
		return nil
	}
	slot := &v.slots[index]
	if slot.state != slotLive || slot.voice != voice {
		return nil
	}
	return slot
}

// start applies one recorded play. The Voice exists from here: addressable,
// Stop-able, and silent until its Clip is resident.
//
// A play whose handle names no slot is the incoming play losing the cap. It
// still got a real handle, and its ending arrives in this same flush.
//
// A play whose handle names a slot another Voice is still holding is a ranked
// steal: the minter chose that slot when the play was recorded, and the Voice
// on it ends here with ReasonStolen. The choice is not remade here, because the
// handle already carries it - see minter.mint for why that is where it has to
// be made.
func (v *Voices) start(op Operation, clip clipFacts, endings *[]Ending) {
	index := int(op.Voice.idx())
	if index >= len(v.slots) {
		*endings = append(*endings, Ending{Voice: op.Voice, Reason: ReasonStolen})
		return
	}
	slot := &v.slots[index]
	if slot.emitted {
		// The Adapter is holding a Voice here, so it is stopped before the slot
		// is reused, whether the Voice it held is being stolen now or ended
		// earlier in this same tick. Nothing about stealing crosses the seam:
		// what the Adapter gets is an ordinary stop, ordered before the start
		// that takes the slot over.
		v.vacated = append(v.vacated, VoiceSlot(index))
	}
	if slot.state == slotLive {
		*endings = append(*endings, Ending{Voice: slot.voice, Reason: ReasonStolen})
		v.live--
	}
	v.sequence++
	*slot = voiceSlot{
		state:    slotLive,
		voice:    op.Voice,
		clip:     op.Clip,
		bus:      op.Params.Bus.Or(Master).resolve(),
		params:   op.Params,
		offset:   float64(op.Offset),
		playhead: float64(op.Offset),
		born:     v.sequence,
		pending:  true,
		fresh:    true,
	}
	v.live++
	if clip.state == ClipReady {
		slot.bind(clip)
	}
}

// stop ends a Voice. It is a no-op on a Voice that is gone, which covers a Stop
// racing a Clip that finished a tick ago and a second Stop in one tick alike.
func (v *Voices) stop(voice Voice, endings *[]Ending) {
	slot := v.slotOf(voice)
	if slot == nil {
		return
	}
	slot.state = slotEnded
	v.live--
	*endings = append(*endings, Ending{Voice: voice, Reason: ReasonStopped})
}

// set applies one recorded SetVoice. An absent field is unchanged.
//
// Changing Loop on a Voice the Adapter already holds restarts it where it
// stands. Loop is a fact of a VoiceStart at the seam and VoiceUpdate has no
// field for it, so a restart carrying the new flag is the only sentence the
// seam can say; the alternative is sound's playhead wrapping while the
// Adapter's does not, which is a Voice that goes silent while the view insists
// it is playing. It costs a block-accurate discontinuity at the moment the game
// toggles looping, which is what a Seek costs and for the same reason.
func (v *Voices) set(voice Voice, params Params) {
	slot := v.slotOf(voice)
	if slot == nil {
		return
	}
	looping := slot.looping()
	slot.params = slot.params.merge(params)
	if bus, ok := params.Bus.Get(); ok {
		slot.bus = bus.resolve()
	}
	slot.changed = true
	if slot.looping() != looping && !slot.pending {
		slot.offset = slot.playhead
		slot.started = true
	}
}

// seek moves a Voice's playhead. It is a no-op on a Voice that is gone.
//
// A negative offset clamps to zero. An offset past the end ends a one-shot with
// ReasonFinished and wraps a looping Voice to its loop start, never to zero -
// which is where the Clip's Loop Region is felt on the game face: a seek off
// the end of a track with an intro lands in the loop rather than replaying the
// intro. The end it is past is the Clip's, not the loop's: a seek is where the
// game asked to be, and a looping Voice put down inside its tail is carried
// back into its span by the next wrap rather than refused here.
//
// What crosses the seam is a VoiceStart carrying the new Offset, which is how a
// web Adapter's sample-accurate start(when, offset) is reached without the seam
// ever naming a sample. A Voice still waiting on its Clip is seeked by moving
// the offset it will start from: it has no playhead to move and no start to
// re-emit, and it was going to begin from its offset with no catch-up anyway.
func (v *Voices) seek(voice Voice, offset float32, endings *[]Ending) {
	slot := v.slotOf(voice)
	if slot == nil {
		return
	}
	at := float64(offset)
	if at < 0 {
		at = 0
	}
	if slot.pending {
		slot.offset, slot.playhead = at, at
		return
	}
	if at >= slot.duration {
		if !slot.looping() {
			slot.playhead = slot.duration
			slot.state = slotEnded
			v.live--
			*endings = append(*endings, Ending{Voice: voice, Reason: ReasonFinished})
			return
		}
		at = slot.loopStart()
	}
	slot.offset, slot.playhead = at, at
	slot.started = true
}

// setEnginePaused records an engine Pause over the whole table and reports
// whether it changed anything.
//
// It marks every live Voice as owed an update, because that is all an engine
// Pause is at the seam: an update for every live Voice, at most MaxVoices
// entries and no special path. A SuspendAll verb would be a second way to say
// what the batch already says, and two ways to say one thing can disagree.
func (v *Voices) setEnginePaused(paused bool) bool {
	if v.enginePaused == paused {
		return false
	}
	v.enginePaused = paused
	for i := range v.slots {
		if v.slots[i].state == slotLive {
			v.slots[i].changed = true
		}
	}
	return true
}

// stopBus ends every Voice on a Bus, with ReasonStopped rather than a member of
// its own. Master stops everything, because every Bus is directly under it.
//
// It is a stop over a set and nothing more: a Voice it reaches ends exactly as
// a Stop naming it would have ended it, and a Voice that began and ended inside
// one tick is still never audible.
func (v *Voices) stopBus(bus Bus, endings *[]Ending) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive || (bus != Master && slot.bus != bus) {
			continue
		}
		slot.state = slotEnded
		v.live--
		*endings = append(*endings, Ending{Voice: slot.voice, Reason: ReasonStopped})
	}
}

// foldBuses folds each Bus's volume into each Voice's gain, which is the whole
// of what a Bus is: sound decides the volume once, here, above the seam, and
// nothing about a Bus appears in a Batch, a VoiceStart or a VoiceParams.
//
// The cost is stated rather than optimised away: a Bus volume change re-emits
// every Voice on that Bus, bounded by MaxVoices and so by at most 64 entries in
// one batch. That is nothing against the alternative, which is a second place
// volume is decided - and the Voices that were already at that volume are not
// re-emitted, because a fold that did not move is not a change.
func (v *Voices) foldBuses(buses *Buses) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive || slot.busGain == buses.volumes[slot.bus] {
			continue
		}
		slot.busGain = buses.volumes[slot.bus]
		slot.changed = true
	}
}

// spatializeAll runs the W3C equations over every live Voice against the one
// Listener, which is where a position turns into a falloff, a cone and a
// bearing.
//
// It is a pass of its own rather than arithmetic inside collect because the
// Listener moving changes every Positional Voice at once, with no operation
// naming any of them: a player turning on the spot re-emits the sources around
// them, and a Voice whose bearing did not move is not re-emitted. That is the
// same "a fold that did not move is not a change" the Buses run on, which is
// why the two passes look alike.
func (v *Voices) spatializeAll(listener *Listener) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive {
			continue
		}
		if slot.spatialize(listener) {
			slot.changed = true
		}
	}
}

// resolve binds every Voice still waiting on its Clip, and ends the ones whose
// Clip cannot be read or prepared.
//
// A failure ends a Voice one or more ticks after the Play and never in the same
// tick, which is what the fresh flag buys: a game cannot write "play it, and if
// it fails this frame, do X".
func (v *Voices) resolve(lookup func(ClipRef) clipFacts, endings *[]Ending) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive || !slot.pending {
			continue
		}
		switch facts := lookup(slot.clip); facts.state {
		case ClipReady:
			slot.bind(facts)
		case ClipFailed:
			if slot.fresh {
				continue
			}
			slot.state = slotEnded
			v.live--
			*endings = append(*endings, Ending{Voice: slot.voice, Reason: ReasonFailed})
		}
	}
}

// advance moves every playhead by one tick and ends the Voices that ran out.
//
// A playhead advances whether or not anyone can hear it, which is what keeps a
// test's timeline world time rather than the Device's. A paused Voice suspends:
// the playhead stops and resumes on the same sample. A pending Voice does not
// advance at all - it starts from its offset when the Clip installs, with no
// catch-up. A looping Voice wraps rather than ending, and never ends by itself.
//
// An engine Pause suspends every Voice the same way, the playhead rule
// included, and it beats a Device that is not ready: nothing advances. It is
// read here rather than only at the seam because a step under pause still
// publishes a tick, and a stepped tick that moved a suspended playhead would
// resume the music somewhere the Adapter is not.
//
// It moves by dt scaled by the Voice's rate, which is what keeps sound's ending
// in step with the Mixer's sample-exact playhead. Unscaled, a Voice at twice
// its rate would run out in the Mixer after half its duration and be cut there
// rather than stopped and declicked here.
func (v *Voices) advance(dt float64, endings *[]Ending) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive || slot.pending || slot.suspended(v.enginePaused) {
			continue
		}
		slot.playhead += dt * float64(slot.rate())
		if slot.looping() {
			slot.wrap()
			continue
		}
		if slot.playhead < slot.duration {
			continue
		}
		slot.playhead = slot.duration
		slot.state = slotEnded
		v.live--
		*endings = append(*endings, Ending{Voice: slot.voice, Reason: ReasonFinished})
	}
}

// collect fills one tick's batch from the table: a start for every Voice bound
// in this flush, an update for every Voice whose Params moved, and a stop for
// every slot the Adapter still holds a Voice on.
//
// A Voice played and ended inside one flush appears in neither, which is the
// atomic-per-tick promise seen from below: it was never audible, so there is
// nothing to start and nothing to stop.
//
// resync is the Device having just become ready, and it restates every live
// Voice as a start at the playhead it is at now. It is what "on recovery,
// Voices resume where the world is now" costs above the seam: the playhead
// advanced through the outage whether or not anyone could hear it, and an
// Adapter whose mixer table survived the loss would otherwise resume each Voice
// where it was when the Device went away. A start is the only operation that
// carries a position, so a restart is the resync.
func (v *Voices) collect(batch *Batch, resync bool) {
	// The slots a start took over go first, so the stop a stolen Voice is owed
	// is stated before the start that reuses its slot rather than after it. An
	// Adapter's table is a fixed array indexed by slot and its two operations
	// on one entry in one batch are not commutative.
	batch.Stops = append(batch.Stops, v.vacated...)
	for i := range v.slots {
		slot := &v.slots[i]
		switch {
		case slot.state == slotEnded:
			if slot.emitted {
				batch.Stops = append(batch.Stops, VoiceSlot(i))
			}
		case slot.state != slotLive || slot.pending:
		case slot.started:
			batch.Starts = append(batch.Starts, VoiceStart{
				Slot:   VoiceSlot(i),
				Clip:   slot.clipID,
				Offset: time.Duration(slot.offset * float64(time.Second)),
				Loop:   slot.looping(),
				Params: slot.voiceParams(v.enginePaused),
			})
			slot.emitted = true
		case resync:
			// The start carries this tick's Params, so a Voice whose Params
			// also moved needs no update beside it. A Voice the Adapter never
			// heard of is started rather than skipped: it is the same
			// operation either way, and a slot the Adapter has forgotten is
			// exactly what a restart is for.
			batch.Starts = append(batch.Starts, VoiceStart{
				Slot:   VoiceSlot(i),
				Clip:   slot.clipID,
				Offset: time.Duration(slot.playhead * float64(time.Second)),
				Loop:   slot.looping(),
				Params: slot.voiceParams(v.enginePaused),
			})
			slot.emitted = true
		case slot.changed && slot.emitted:
			batch.Updates = append(batch.Updates, VoiceUpdate{
				Slot:   VoiceSlot(i),
				Params: slot.voiceParams(v.enginePaused),
			})
		}
	}
}

// endTick clears what only this flush meant: the slots whose Voices ended, and
// the per-tick flags of the ones that did not.
func (v *Voices) endTick() {
	v.vacated = v.vacated[:0]
	for i := range v.slots {
		slot := &v.slots[i]
		switch slot.state {
		case slotEnded:
			*slot = voiceSlot{}
		case slotLive:
			slot.fresh, slot.started, slot.changed = false, false, false
		}
	}
}

// bind attaches a resident Clip to a Voice that was waiting on it. The Voice
// starts from its offset: a 300 ms load must not play a footstep's tail.
//
// It is also where the Clip's Loop Region becomes two numbers, so that no tick
// ever unwraps a Maybe: absent is the whole Clip, and a region the Clip's own
// duration does not contain is the whole Clip too. That last guard is not
// distrust of the Adapter, which drops a malformed region itself and reports
// it; it is that wrap subtracts a span in a loop, and a span that is not inside
// the Clip is a loop with no reason to stop.
func (s *voiceSlot) bind(clip clipFacts) {
	s.pending = false
	s.clipID = clip.id
	s.duration = float64(clip.duration)
	s.channels = clip.channels
	s.loopFrom, s.loopTo = 0, s.duration
	if region, ok := clip.region.Get(); ok {
		start, end := float64(region.Start), float64(region.End)
		if start >= 0 && end > start && end <= s.duration {
			s.loopFrom, s.loopTo = start, end
		}
	}
	s.playhead = s.offset
	s.started = true
}
