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
	// Paused is Params.Paused resolved, so a reader does not have to.
	Paused bool
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
	channels int
	clipID   ClipID
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
func (s *voiceSlot) info() VoiceInfo {
	return VoiceInfo{
		Voice:    s.voice,
		Clip:     s.clip,
		Bus:      s.bus,
		Playhead: float32(s.playhead),
		Duration: float32(s.duration),
		Params:   s.params,
		Paused:   s.params.Paused.Or(false),
	}
}

// voiceParams is what the slot looks like below the seam.
//
// A Voice with no position is non-positional and heard centred: no falloff, no
// cone and no panning, so the matrix is the identity scaled by the volume - one
// row for a mono Clip, a diagonal for a stereo one. The W3C equations that fill
// it for a Positional Voice are issue 479.
func (s *voiceSlot) voiceParams() VoiceParams {
	volume := s.params.Volume.Or(1)
	var gains [2][2]float32
	if s.channels >= 2 {
		gains[0][0], gains[1][1] = volume, volume
	} else {
		gains[0][0], gains[0][1] = volume, volume
	}
	return VoiceParams{Gains: gains, Rate: 1, Paused: s.params.Paused.Or(false)}
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
	return slot.info(), true
}

// All yields every live Voice, in slot order.
func (v *Voices) All() iter.Seq[VoiceInfo] {
	return func(yield func(VoiceInfo) bool) {
		for i := range v.slots {
			if v.slots[i].state != slotLive {
				continue
			}
			if !yield(v.slots[i].info()) {
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
func (v *Voices) start(op Operation, clip clipFacts, endings *[]Ending) {
	index := int(op.Voice.idx())
	if index >= len(v.slots) {
		*endings = append(*endings, Ending{Voice: op.Voice, Reason: ReasonStolen})
		return
	}
	slot := &v.slots[index]
	*slot = voiceSlot{
		state:    slotLive,
		voice:    op.Voice,
		clip:     op.Clip,
		bus:      op.Params.Bus.Or(Master),
		params:   op.Params,
		offset:   float64(op.Offset),
		playhead: float64(op.Offset),
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
func (v *Voices) set(voice Voice, params Params) {
	slot := v.slotOf(voice)
	if slot == nil {
		return
	}
	slot.params = slot.params.merge(params)
	if bus, ok := params.Bus.Get(); ok {
		slot.bus = bus
	}
	slot.changed = true
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
// catch-up.
func (v *Voices) advance(dt float64, endings *[]Ending) {
	for i := range v.slots {
		slot := &v.slots[i]
		if slot.state != slotLive || slot.pending || slot.params.Paused.Or(false) {
			continue
		}
		slot.playhead += dt
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
				Params: slot.voiceParams(),
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
				Params: slot.voiceParams(),
			})
			slot.emitted = true
		case slot.changed && slot.emitted:
			batch.Updates = append(batch.Updates, VoiceUpdate{Slot: VoiceSlot(i), Params: slot.voiceParams()})
		}
	}
}

// endTick clears what only this flush meant: the slots whose Voices ended, and
// the per-tick flags of the ones that did not.
func (v *Voices) endTick() {
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
func (s *voiceSlot) bind(clip clipFacts) {
	s.pending = false
	s.clipID = clip.id
	s.duration = float64(clip.duration)
	s.channels = clip.channels
	s.playhead = s.offset
	s.started = true
}
