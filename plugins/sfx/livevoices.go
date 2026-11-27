package sfx

import (
	"iter"
	"time"

	"github.com/dvoyni/cog/libs/m"
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
	return slot.makeVoiceEntry(v.table.paused), true
}

// All yields every live voice, in slot order.
func (v liveVoices) All() iter.Seq[VoiceEntry] {
	return func(yield func(VoiceEntry) bool) {
		for index := range v.table.slots {
			if slot := &v.table.slots[index]; slot.state.live() && !yield(slot.makeVoiceEntry(v.table.paused)) {
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
// offset. A play whose handle names no slot found every slot taken and
// nothing it outranked, and ends here with ReasonStolen.
//
// A play whose handle names a slot another voice still holds is a steal: the
// queue chose that voice when the play was recorded, and it ends here with
// ReasonStolen, the choice not remade. A voice the backend holds in the slot,
// stolen now or ended earlier in this flush, is owed its stop, appended to
// stops here, which the backend applies before the start that takes the slot
// over: nothing of stealing crosses the seam but an ordinary stop.
func (v liveVoices) start(play operation, clip clipEntry, endings *[]VoiceEndedEvent, stops *[]VoiceSlot) {
	index := int(play.voice.index())
	if index >= len(v.table.slots) {
		*endings = append(*endings, VoiceEndedEvent{Voice: play.voice, Reason: ReasonStolen})
		return
	}
	slot := &v.table.slots[index]
	if slot.state.live() {
		v.end(slot, ReasonStolen, endings)
	}
	if slot.state == slotStopping {
		*stops = append(*stops, VoiceSlot(index))
	}
	v.table.startCount++
	*slot = tableSlot{
		state:       slotWaiting,
		voice:       play.voice,
		path:        play.path,
		bus:         play.bus,
		offset:      play.offset,
		options:     play.options,
		playhead:    play.offset,
		startNumber: v.table.startCount,
	}
	v.table.liveCount++
	if clip.state == ClipReady {
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

// set applies one recorded SetVoice, as Queue.SetVoice says. A change of
// Loop restarts a voice the backend holds where it stands, since a
// VoiceUpdate has no field for it: otherwise sfx's playhead would wrap while
// the backend's ran out, a voice gone silent while the view says it plays.
func (v liveVoices) set(change operation) {
	slot := v.findSlot(change.voice)
	if slot == nil {
		return
	}
	wasLooping := slot.looping()
	slot.options = slot.options.merge(change.options)
	if change.options.Bus.Present() {
		slot.bus = change.bus
	}
	if slot.looping() != wasLooping {
		slot.restartFrom(slot.playhead)
	}
}

// seek applies one recorded Seek, as Queue.Seek says. A voice still waiting
// on its clip has no end to be past, and moves only the offset it will start
// from. The end a seek is past is the clip's, not the loop's, so a looping
// voice put down in its tail is carried back into its region by the next
// wrap. A voice the backend holds is restarted at the new offset, which is
// how the seam says a seek.
func (v liveVoices) seek(voice Voice, offset time.Duration, endings *[]VoiceEndedEvent) {
	slot := v.findSlot(voice)
	if slot == nil {
		return
	}
	offset = max(offset, 0)
	if slot.state != slotWaiting && offset >= slot.duration {
		if !slot.looping() {
			slot.playhead = slot.duration
			v.end(slot, ReasonFinished, endings)
			return
		}
		offset = slot.loopStart
	}
	slot.offset = offset
	slot.playhead = offset
	slot.restartFrom(offset)
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

// release ends every live voice playing the clip at path with
// ReasonReleased, one still waiting on it included: nothing failed, the game
// changed its mind, and it is the game's own verb, so the flush that applies
// it answers it.
func (v liveVoices) release(path string, endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state.live() && slot.path == path {
			v.end(slot, ReasonReleased, endings)
		}
	}
}

// releaseAll ends every live voice with ReasonReleased, as release does each
// voice on a clip.
func (v liveVoices) releaseAll(endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state.live() {
			v.end(slot, ReasonReleased, endings)
		}
	}
}

// restate owes every voice the backend holds a restart at its playhead, as
// the device turning ready does: the backend has no playheads, so only sfx
// can resume a voice where the world is now rather than where the device
// left it. It costs at most MaxVoices starts, once per arrival.
func (v liveVoices) restate() {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		slot.restartFrom(slot.playhead)
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
		case ClipReady:
			slot.bind(clip)
		case ClipFailed:
			if !playedThisFlush {
				v.end(slot, ReasonFailed, endings)
			}
		}
	}
}

// advance moves every playhead by delta scaled by its voice's rate, the rate
// computeParams sends the backend, whether or not anyone can hear it, so a
// test's timeline is world time, and ends with ReasonFinished each voice that
// ran out. Scaling keeps sfx's ending in step with the mixer: a voice at twice
// its rate runs out after half its clip. A looping voice wraps at its loop end
// instead, and never ends by itself. A voice waiting on its clip does not
// advance: it starts from its offset once the clip is ready, with no catch-up.
// A suspended voice does not advance either, so it resumes on the same sample.
func (v liveVoices) advance(delta time.Duration, endings *[]VoiceEndedEvent) {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		if slot.state != slotStarting && slot.state != slotSounding || slot.suspended(v.table.paused) {
			continue
		}
		slot.playhead += time.Duration(float64(delta) * float64(max(slot.options.Pitch.Or(1), 0)))
		if slot.looping() {
			slot.wrap()
			continue
		}
		if slot.playhead >= slot.duration {
			slot.playhead = slot.duration
			v.end(slot, ReasonFinished, endings)
		}
	}
}

// computeParams computes every live voice's params, the whole of how it
// sounds, from what the flush has applied: the W3C equations against the one
// listener, heardFrom, give its distance, its falloff and cone and the
// panning of its azimuth; buses gives its bus's volume, folded into its
// audibility and so into its gains; and the pause of every voice, beside its
// own, gives whether it is paused. The listener moving, a bus's volume and
// the pause each change voices no operation names, which is why they are
// computed in one pass of their own, and why the params alone say whether
// the backend is owed an update. No bus reaches the backend: sfx decides the
// volume here, above the seam.
func (v liveVoices) computeParams(heardFrom listener, buses busVolumes) {
	front, up := heardFrom.resolveAxes()
	for index := range v.table.slots {
		if slot := &v.table.slots[index]; slot.state.live() {
			slot.computeParams(heardFrom.position, front, up, buses.volumes[slot.bus], v.table.paused)
		}
	}
}

// collect fills changes from the table: a start for every voice started in
// this flush, carrying its params and whether it loops, an update for every
// voice the backend holds whose params differ from those the backend last
// had of it, and a stop for every voice that ended while the backend held
// it, emptying the slots of the stopped. Comparing the params is the one test
// of whether a voice is restated, whatever moved them: a SetBus, a
// listener's move or a pause costs at most MaxVoices updates, and a voice
// whose params came out the same, at a bus volume it already had, a listener
// it did not move relative to or a pause it was already under, costs none. A
// voice owed a restart, by a seek, a change of Loop or the device turning
// ready, is started again at its offset instead, with this flush's params,
// which it then holds, so no update follows it. A voice played and ended
// inside one flush appears in none: it was never audible, so there is
// nothing to start and nothing to stop.
func (v liveVoices) collect(changes *FlushChanges) {
	for index := range v.table.slots {
		slot := &v.table.slots[index]
		switch slot.state {
		case slotStopping:
			changes.Stops = append(changes.Stops, VoiceSlot(index))
			*slot = tableSlot{}
		case slotStarting, slotSounding:
			switch {
			case slot.state == slotStarting || slot.owesRestart:
				changes.Starts = append(changes.Starts, VoiceStart{
					Slot:   VoiceSlot(index),
					Clip:   slot.clipID,
					Offset: slot.offset,
					Loop:   slot.looping(),
					Params: slot.params,
				})
				slot.state = slotSounding
				slot.owesRestart = false
			case slot.params != slot.heldParams:
				changes.Updates = append(changes.Updates, VoiceUpdate{Slot: VoiceSlot(index), Params: slot.params})
			}
			slot.heldParams = slot.params
		}
	}
}

// end ends the live voice in slot for reason. A voice the backend holds keeps
// its slot until the flush's changes are collected, or a play takes the slot
// over, so the backend gets its stop; any other is emptied at once.
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
	// startCount counts the plays the table has started, which stamps each
	// voice's startNumber.
	startCount uint64
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
	// backend held it, owed a stop, until the flush's changes are collected
	// or a play takes the slot over, either handing the backend the stop.
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
	// offset is where in the clip the backend's next start of the voice
	// begins it: where its play began it, until a seek, a change of Loop or
	// a restart moves it. playhead is where the voice is.
	offset   time.Duration
	playhead time.Duration
	// options is what the voice is set to now: how it was played, with every
	// SetVoice merged in.
	options VoiceOptions
	// owesRestart is whether the backend holds the voice and a seek, a change
	// of Loop or restate marked it, so it is owed a VoiceStart at its offset
	// in place of an update.
	owesRestart bool
	// startNumber is the voice's place in the order the table started voices
	// in, which "ties broken by age, oldest first" compares. It counts starts
	// rather than flushes, since two voices played before one flush at one
	// position tie on every other term exactly.
	startNumber uint64
	// clipID, duration and channels are the clip's, zero while the voice
	// waits on it, and loopStart and loopEnd the span a looping voice repeats
	// between: the clip's loop region, or the whole clip when it declares
	// none or one the clip does not contain.
	clipID    ClipID
	duration  time.Duration
	channels  int
	loopStart time.Duration
	loopEnd   time.Duration
	// distance is how far the voice is from the listener, zero on a
	// non-positional voice, and audibility the scalar it is heard at before
	// panning, its falloff, its cone, its own volume and its bus's, as of the
	// last computeParams pass.
	distance   float32
	audibility float32
	// params is how the voice sounds as of the last computeParams pass, and
	// heldParams the params the backend last had of it, so a voice whose
	// params did not move is not restated.
	params     VoiceParams
	heldParams VoiceParams
}

// makeVoiceEntry answers the slot's voice as LiveVoices shows it, under the
// pause of every voice, paused.
func (s *tableSlot) makeVoiceEntry(paused bool) VoiceEntry {
	return VoiceEntry{
		Voice:      s.voice,
		Path:       s.path,
		Bus:        s.bus,
		Playhead:   s.playhead,
		Duration:   s.duration,
		Options:    s.options,
		Paused:     s.suspended(paused),
		Audibility: s.audibility,
		Distance:   s.distance,
	}
}

// computeParams computes the slot's distance, audibility and params as heard
// by a listener at listenerAt whose axes are front and up, on a bus at
// busVolume, under the pause of every voice, paused. A voice with no position
// is non-positional: no falloff, no cone, and the panning of azimuth 0, which
// is "heard centred" as a place on the circle rather than a second rule. A
// positional voice is as loud as its falloff and, when it has a facing, its
// cone say. Its audibility is that times its own volume and its bus's, and
// the equal-power matrix of its azimuth is scaled by it. Its rate is its
// pitch, 1 being the clip's own, and a negative pitch is 0, a legitimate
// freeze, since it would run the voice backwards off the front of its clip.
func (s *tableSlot) computeParams(listenerAt, front, up m.Vec3, busVolume float32, paused bool) {
	s.audibility, s.distance = s.options.computeAudibility(listenerAt, busVolume)
	azimuth := 0.0
	if position, positional := s.options.Position.Get(); positional {
		azimuth, _ = computeAzimuthElevation(position, listenerAt, front, up)
	}
	gains := computeEqualPowerGains(azimuth, s.channels >= 2)
	for source := range gains {
		for output := range gains[source] {
			gains[source][output] *= s.audibility
		}
	}
	s.params = VoiceParams{Gains: gains, Rate: max(s.options.Pitch.Or(1), 0), Paused: s.suspended(paused)}
}

// looping answers whether the voice repeats rather than ending. Where it
// repeats is its clip's to say: loopStart and loopEnd.
func (s *tableSlot) looping() bool { return s.options.Loop.Or(false) }

// wrap brings a looping voice's playhead past its loop end back inside its
// loop region, by whole spans, so a rate that overshoots by several spans in
// one flush still lands inside it. A playhead short of the loop end is the
// intro or the loop itself, and neither is a wrap: a looping voice starts at
// the head of its clip and runs into its loop, carried forwards into it and
// never lifted up to it.
func (s *tableSlot) wrap() {
	if s.playhead < s.loopEnd {
		return
	}
	span := s.loopEnd - s.loopStart
	if span <= 0 {
		s.playhead = s.loopStart
		return
	}
	s.playhead = s.loopStart + (s.playhead-s.loopStart)%span
}

// restartFrom owes the backend a restart of the voice at offset, when it
// holds the voice: the one way the seam says a voice's playhead or looping
// moved. A voice it does not hold yet starts from its offset anyway.
func (s *tableSlot) restartFrom(offset time.Duration) {
	if s.state == slotSounding {
		s.offset = offset
		s.owesRestart = true
	}
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
	s.duration = clip.info.Duration
	s.channels = clip.info.Channels
	s.playhead = s.offset
	s.loopStart = clip.loopStart
	s.loopEnd = clip.loopEnd
}
