package sfx

import (
	"iter"
	"time"
)

// liveVoices is what LiveVoices names: a fixed table of slots, one per voice
// that may exist at once, the handle's index being the slot. It holds one
// private pointer, so Get copies a word.
type liveVoices struct{ table *voiceTable }

// makeLiveVoices answers a table of maxVoices empty slots.
func makeLiveVoices(maxVoices int) liveVoices {
	return liveVoices{table: &voiceTable{slots: make([]tableSlot, maxVoices)}}
}

// Info answers the live voice voice, and whether it exists. A handle whose
// generation its slot has moved past addresses nothing, so a game holding a
// handle across a stop and a reuse of its index never reads the voice that
// took the slot.
func (v liveVoices) Info(voice Voice) (VoiceEntry, bool) {
	slot := v.findSlot(voice)
	if slot == nil {
		return VoiceEntry{}, false
	}
	return VoiceEntry{Voice: slot.voice, Path: slot.path, Bus: slot.bus, Playhead: slot.playhead, Duration: slot.duration, Audibility: slot.busGain, Paused: slot.suspended(v.table.paused)}, true
}

// All yields every live voice, in slot order.
func (v liveVoices) All() iter.Seq[VoiceEntry] {
	return func(yield func(VoiceEntry) bool) {
		for index := range v.table.slots {
			if slot := &v.table.slots[index]; slot.state.live() && !yield(VoiceEntry{Voice: slot.voice, Path: slot.path, Bus: slot.bus, Playhead: slot.playhead, Duration: slot.duration, Audibility: slot.busGain, Paused: slot.suspended(v.table.paused)}) {
				return
			}
		}
	}
}

// Len answers how many voices are live.
func (v liveVoices) Len() int { return v.table.liveCount }

// Cap answers how many voices may exist at once, Config.MaxVoices, beside Len
// so a reader asking whether the game is at its cap gets both numbers under
// one read lock.
func (v liveVoices) Cap() int { return len(v.table.slots) }

// findSlot answers the slot voice addresses, or nil when it addresses
// nothing: NoVoice, a handle minted at a full table, which names no slot, or
// one whose slot no longer holds it.
func (v liveVoices) findSlot(voice Voice) *tableSlot {
	index := int(voice.index())
	if voice == NoVoice || index >= len(v.table.slots) {
		return nil
	}
	slot := &v.table.slots[index]
	if !slot.state.live() || slot.voice != voice {
		return nil
	}
	return slot
}

// start applies one recorded play of clip. The voice exists from here,
// addressable and silent until its clip is ready, and then starts from its
// offset. A play whose handle names no slot found every slot taken, and ends
// here with ReasonStolen.
func (v liveVoices) start(play operation, clip clipEntry, endings *[]VoiceEndedEvent) {
	index := int(play.voice.index())
	if index >= len(v.table.slots) {
		*endings = append(*endings, VoiceEndedEvent{Voice: play.voice, Reason: ReasonStolen})
		return
	}
	slot := &v.table.slots[index]
	*slot = tableSlot{
		state:    slotWaiting,
		voice:    play.voice,
		path:     play.path,
		bus:      play.bus,
		offset:   play.offset,
		options:  play.options,
		playhead: play.offset,
	}
	v.table.liveCount++
	if clip.state == clipReady {
		slot.bind(clip)
	} else {
		slot.playedThisFlush = true
	}
}

// stop ends voice with ReasonStopped. It is a no-op on a voice that is gone,
// which covers a stop racing a clip that finished and a second stop in one
// flush alike.
func (v liveVoices) stop(voice Voice, endings *[]VoiceEndedEvent) {
	if slot := v.findSlot(voice); slot != nil {
		v.end(slot, ReasonStopped, endings)
	}
}

// stopBus ends every live voice on bus with ReasonStopped, as a Stop naming
// each would, and every live voice when bus is Master, since every bus sits
// directly under it.
func (v liveVoices) stopBus(bus Bus, endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state.live() && (bus == Master || slot.bus == bus) {
			v.end(slot, ReasonStopped, endings)
		}
	}
}

// setPaused applies the pause of every voice, or its end. A change owes
// every voice the backend holds an update, since that is all the pause is at
// the seam: at most MaxVoices updates, with no verb of its own. A voice
// started in this flush is owed none, its start carrying the pause.
func (v liveVoices) setPaused(paused bool) {
	if v.table.paused == paused {
		return
	}
	v.table.paused = paused
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state == slotSounding {
			slot.owesUpdate = true
		}
	}
}

// restate owes every voice the backend holds a restart at its playhead, as
// the device turning ready does: the backend has no playheads, so only sfx
// can resume a voice where the world is now rather than where the device
// left it. It costs at most MaxVoices starts, once per arrival.
func (v liveVoices) restate() {
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state == slotSounding {
			slot.owesRestart = true
		}
	}
}

// resolve binds each voice waiting on a clip that clips now holds ready, and
// ends with ReasonFailed each whose clip has failed, unless its play was
// applied in this flush: a failure ends a voice one or more flushes after its
// play and never in the same one, so a game cannot write "play it, and if it
// fails this frame, do X".
func (v liveVoices) resolve(clips clipTable, endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		if slot.state != slotWaiting {
			continue
		}
		playedThisFlush := slot.playedThisFlush
		slot.playedThisFlush = false
		switch clip := clips.state.entries[slot.path]; clip.state {
		case clipReady:
			slot.bind(clip)
		case clipFailed:
			if !playedThisFlush {
				v.end(slot, ReasonFailed, endings)
			}
		}
	}
}

// advance moves every playhead by delta, whether or not anyone can hear it,
// so a test's timeline is world time, and ends with ReasonFinished each voice
// that ran out. A voice waiting on its clip does not advance: it starts from
// its offset once the clip is ready, with no catch-up. A suspended voice does
// not advance either, so it resumes on the same sample.
func (v liveVoices) advance(delta time.Duration, endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		if slot.state != slotStarting && slot.state != slotSounding || slot.suspended(v.table.paused) {
			continue
		}
		slot.playhead += delta
		if slot.playhead >= slot.duration {
			slot.playhead = slot.duration
			v.end(slot, ReasonFinished, endings)
		}
	}
}

// collect folds each live voice's bus volume from buses into its gain, and
// fills changes from the table: a start for every voice started in this
// flush, an update for every voice the backend holds whose fold moved or
// whose pause changed, and a stop for every voice that ended while the
// backend held it, emptying the slots of the stopped. A voice restate
// marked is restarted at its playhead instead, with this flush's params and
// no update beside it. The fold is the whole
// of what a bus is: sfx decides the volume here, above the seam, and no bus
// reaches the backend. A SetBus costs at most MaxVoices updates, and a fold
// that did not move is no change, so it costs none. A voice played and ended
// inside one flush appears in neither the starts nor the stops: it was never
// audible, so there is nothing to start and nothing to stop.
func (v liveVoices) collect(changes *FlushChanges, buses busVolumes) {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		moved := false
		if slot.state.live() {
			gain := buses.volumes[slot.bus]
			moved = gain != slot.busGain
			slot.busGain = gain
		}
		switch slot.state {
		case slotStopping:
			changes.Stops = append(changes.Stops, VoiceSlot(index))
			*slot = tableSlot{}
		case slotStarting:
			params := makeCentredVoiceParams(slot.channels, slot.busGain)
			params.Paused = slot.suspended(v.table.paused)
			changes.Starts = append(changes.Starts, VoiceStart{
				Slot:   VoiceSlot(index),
				Clip:   slot.clipID,
				Offset: slot.offset,
				Params: params,
			})
			slot.state = slotSounding
		case slotSounding:
			if !slot.owesRestart && !moved && !slot.owesUpdate {
				break
			}
			params := makeCentredVoiceParams(slot.channels, slot.busGain)
			params.Paused = slot.suspended(v.table.paused)
			if slot.owesRestart {
				changes.Starts = append(changes.Starts, VoiceStart{Slot: VoiceSlot(index), Clip: slot.clipID, Offset: slot.playhead, Params: params})
			} else {
				changes.Updates = append(changes.Updates, VoiceUpdate{Slot: VoiceSlot(index), Params: params})
			}
			slot.owesRestart = false
			slot.owesUpdate = false
		}
	}
}

// appendFreeSlots answers freeSlots with every slot no voice holds appended,
// the last first, so a queue popping from its end hands out slot 0 first. It
// is called once a flush's changes are collected, so a slot is stopped
// before sfx reuses it.
func (v liveVoices) appendFreeSlots(freeSlots []uint32) []uint32 {
	for index := len(v.table.slots) - 1; index >= 0; index-- {
		if v.table.slots[index].state == slotEmpty {
			freeSlots = append(freeSlots, uint32(index))
		}
	}
	return freeSlots
}

// end ends the live voice in slot for reason. A voice the backend holds keeps
// its slot until the flush's changes are collected, so the backend gets its
// stop; any other is emptied at once.
func (v liveVoices) end(slot *tableSlot, reason Reason, endings *[]VoiceEndedEvent) {
	*endings = append(*endings, VoiceEndedEvent{Voice: slot.voice, Reason: reason})
	v.table.liveCount--
	if slot.state == slotSounding {
		slot.state = slotStopping
		return
	}
	*slot = tableSlot{}
}

// voiceTable is the table liveVoices points to.
type voiceTable struct {
	slots []tableSlot
	// liveCount is how many slots hold a live voice.
	liveCount int
	// paused is the pause of every voice Queue.SetPaused recorded, one flag
	// over the table rather than a voice's own, which each slot's options
	// keep apart from it.
	paused bool
}

// slotState is where the voice in one slot of the table is in its life.
type slotState uint8

const (
	// slotEmpty holds no voice.
	slotEmpty slotState = iota
	// slotWaiting holds a voice whose clip is not ready yet: addressable,
	// stoppable and silent, its playhead not advancing.
	slotWaiting
	// slotStarting holds a voice bound to its clip in the flush running,
	// owed a VoiceStart.
	slotStarting
	// slotSounding holds a voice the backend holds in this slot.
	slotSounding
	// slotStopping holds a voice that ended in the flush running while the
	// backend held it, owed a stop, until the flush's changes are collected.
	slotStopping
)

// live answers whether a slot in the state holds a live voice.
func (s slotState) live() bool {
	return s == slotWaiting || s == slotStarting || s == slotSounding
}

// tableSlot is one slot of the table. It holds the whole handle of its
// voice, so a stale handle is told from the live one with one compare.
type tableSlot struct {
	state slotState
	// playedThisFlush is a voice waiting on its clip whose play the flush
	// running applied, which a failed clip does not end until the next.
	playedThisFlush bool
	voice           Voice
	path            string
	bus             Bus
	// busGain is the volume of the voice's bus as of the last fold, zero on a
	// slot the fold has not reached yet.
	busGain float32
	// offset is where in the clip the voice begins, and playhead where it is.
	offset   time.Duration
	playhead time.Duration
	// options is how the voice was played.
	options VoiceOptions
	// owesUpdate is whether the backend holds the voice and its params moved
	// in the flush running, so it is owed a VoiceUpdate.
	owesUpdate bool
	// owesRestart is whether the backend holds the voice and restate marked
	// it, so it is owed a VoiceStart at its playhead in place of an update.
	owesRestart bool
	// clipID, duration and channels are the clip's, zero while the voice
	// waits on it.
	clipID   ClipID
	duration time.Duration
	channels int
}

// suspended answers whether the voice's playhead is stopped: by its own
// VoiceOptions.Paused, or by the pause of every voice, paused.
func (s *tableSlot) suspended(paused bool) bool {
	return paused || s.options.Paused.Or(false)
}

// bind attaches the ready clip to the voice in the slot, which was waiting on
// it, and owes the backend its start. The voice starts from its offset, with
// no catch-up however long it waited: a 300 ms load must not play a
// footstep's tail.
func (s *tableSlot) bind(clip clipEntry) {
	s.state = slotStarting
	s.clipID = clip.id
	s.duration = clip.duration
	s.channels = clip.channels
	s.playhead = s.offset
}
