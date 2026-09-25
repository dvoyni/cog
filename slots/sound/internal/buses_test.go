package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The Buses a game declares. sound declares only Master, and these two are what
// every settings screen ever built has on it.
const (
	music Bus = 1
	sfx   Bus = 2
)

// busVolume reads a Bus's volume the way a settings screen does: under a read
// lock on a resource of its own, never by asking the queue it records into.
func (h *harness) busVolume(bus Bus) float32 {
	h.t.Helper()
	return h.kernel.ExecuteCommand[probeCmd](probeRequest{Bus: bus}).BusVolume
}

// The whole of what a Bus is for: a settings screen moves a slider and the
// music gets quieter while the footsteps do not.
//
// The fold happens above the seam, so it is visible twice - in the audibility
// the view reports, which is what a game's test asserts on, and in the gain
// matrix the Adapter is handed, which carries no Bus of any kind.
func TestABusVolumeFallsOnItsOwnVoicesAndNoOthers(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	var track, footsteps Voice
	h.record(func(queue *Queue) {
		track = queue.Play(ClipWithResource(bell), 0,
			Params{Bus: m.Some(music), Volume: m.Some[float32](0.8)})
		footsteps = queue.Play(ClipWithResource(bell), 0,
			Params{Bus: m.Some(sfx), Volume: m.Some[float32](0.8)})
	})
	h.tick()

	if got := h.probe(track).Info.Audibility; got != 0.8 {
		t.Fatalf("the music is audible at %v before any slider moved, want 0.8", got)
	}

	h.record(func(queue *Queue) { queue.SetBus(music, 0.5) })
	h.tick()

	if got := h.probe(track).Info.Audibility; got != 0.4 {
		t.Fatalf("the music is audible at %v on a Bus at half, want 0.4", got)
	}
	if got := h.probe(footsteps).Info.Audibility; got != 0.8 {
		t.Fatalf("the footsteps are audible at %v, and nothing touched their Bus, want 0.8", got)
	}

	// The Bus volume change re-emits every Voice on that Bus and no other, and
	// what it re-emits is a gain matrix: an Adapter does not know Buses exist.
	batch := h.backend.emitted()[1]
	if len(batch.Updates) != 1 {
		t.Fatalf("the slider produced %d updates, want the one Voice on that Bus", len(batch.Updates))
	}
	if batch.Updates[0].Slot != 0 {
		t.Fatalf("the update is for slot %d, want the music's slot 0", batch.Updates[0].Slot)
	}
	if got := batch.Updates[0].Params.Gains; !sameGains(got, [2][2]float32{{0.4, 0}, {0, 0.4}}) {
		t.Fatalf("the update carries gains %v, want the Bus folded in at 0.4", got)
	}
}

// A Bus volume is readable, so a settings screen draws its slider where the
// player left it - and every Bus starts at unity, so a game that declares none
// is audible and its master slider works.
//
// It is coalesced within the tick, last value winning: a slider dragged through
// a hundred values in one tick is one value by the time anything is folded with
// it, and the Voices on it are re-emitted once.
func TestABusVolumeIsReadableAndCoalescedWithinOneTick(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	if got := h.busVolume(music); got != 1 {
		t.Fatalf("a Bus nobody has set is at %v, want unity", got)
	}

	voice := h.play(ClipWithResource(bell), 0, Params{Bus: m.Some(music)})
	h.tick()

	h.record(func(queue *Queue) {
		for _, dragged := range []float32{0.9, 0.7, 0.3} {
			queue.SetBus(music, dragged)
		}
	})
	h.tick()

	if got := h.busVolume(music); got != 0.3 {
		t.Fatalf("the Bus reads back at %v, want the last value the tick recorded", got)
	}
	if got := h.probe(voice).Info.Audibility; got != 0.3 {
		t.Fatalf("the Voice is audible at %v, want the last value the tick recorded", got)
	}
	if got := h.backend.emitted()[1].Updates; len(got) != 1 {
		t.Fatalf("three values in one tick produced %d updates, want one", len(got))
	}

	// A volume that did not move is not a change, so nothing is re-emitted for
	// it: the cost is bounded by MaxVoices per change, not paid every tick.
	h.record(func(queue *Queue) { queue.SetBus(music, 0.3) })
	h.tick()
	if got := h.backend.emitted()[2].Updates; len(got) != 0 {
		t.Fatalf("setting a Bus to the volume it already had produced %d updates", len(got))
	}
}

// An absent Bus lands on Master, and so does one outside the fixed maximum: a
// mis-declared constant does not silence a game, and the rule is the same
// wherever a Bus is named, so a game whose constant is wrong hears exactly what
// it asked for, one Bus over.
func TestAnAbsentBusAndAnOutOfRangeOneBothLandOnMaster(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	const misdeclared Bus = MaxBuses + 7

	var unset, strayed Voice
	h.record(func(queue *Queue) {
		unset = queue.Play(ClipWithResource(bell), 0, Params{})
		strayed = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(misdeclared)})
	})
	h.tick()

	if got := h.probe(unset).Info.Bus; got != Master {
		t.Fatalf("a Voice with no Bus is on Bus %d, want Master", got)
	}
	if got := h.probe(strayed).Info.Bus; got != Master {
		t.Fatalf("a Voice on Bus %d is on Bus %d, want Master", misdeclared, got)
	}

	// The slider a game wired to its mis-declared constant moves Master, which
	// is the Bus its sounds are actually on.
	h.record(func(queue *Queue) { queue.SetBus(misdeclared, 0.25) })
	h.tick()

	if got := h.busVolume(misdeclared); got != 0.25 {
		t.Fatalf("the out-of-range Bus reads back at %v, want 0.25", got)
	}
	if got := h.busVolume(Master); got != 0.25 {
		t.Fatalf("Master reads back at %v, want the 0.25 the out-of-range SetBus landed on", got)
	}
	if got := h.probe(unset).Info.Audibility; got != 0.25 {
		t.Fatalf("the Voice with no Bus is audible at %v, want Master's 0.25", got)
	}
	if got := h.probe(strayed).Info.Audibility; got != 0.25 {
		t.Fatalf("the Voice on a mis-declared Bus is audible at %v, want Master's 0.25", got)
	}
}

// StopBus ends its Voices with ReasonStopped rather than a member of its own:
// it is Stop over a set, and a reader switching on ReasonStopped keeps working.
// Master stops everything, because every Bus is directly under it.
func TestStopBusEndsItsVoicesWithReasonStoppedAndMasterStopsEverything(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	var first, second, footsteps Voice
	h.record(func(queue *Queue) {
		first = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(music)})
		second = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(music)})
		footsteps = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(sfx)})
	})
	h.tick()

	h.record(func(queue *Queue) { queue.StopBus(music) })
	h.tick()

	for _, voice := range []Voice{first, second} {
		if h.probe(voice).Found {
			t.Fatalf("%v survived a StopBus on its own Bus", voice)
		}
	}
	if !h.probe(footsteps).Found {
		t.Fatalf("%v ended, and the StopBus named another Bus", footsteps)
	}
	for range 2 {
		if ended := h.waitEnded(); ended.Reason != ReasonStopped {
			t.Fatalf("%v ended as %v, want stopped", ended.Voice, ended.Reason)
		}
	}
	if got := h.backend.emitted()[1].Stops; len(got) != 2 {
		t.Fatalf("the StopBus emitted %d stops, want the two Voices on that Bus", len(got))
	}

	h.record(func(queue *Queue) { queue.StopBus(Master) })
	h.tick()

	if got := h.probe(footsteps); got.Found || got.Live != 0 {
		t.Fatalf("a StopBus on Master left %d Voices, and Master stops everything", got.Live)
	}
	if ended := h.waitEnded(); ended.Voice != footsteps || ended.Reason != ReasonStopped {
		t.Fatalf("%v ended as %v, want %v/stopped", ended.Voice, ended.Reason, footsteps)
	}
}

// StopBus is recorded in order rather than coalesced, and this is the sequence
// that says why: a Play recorded before it is stopped, and one recorded after
// it is not. A Bus volume has no such sequence, which is why that one is a
// table the tick's last word wins and this one is an operation in the list.
func TestStopBusStopsThePlaysBeforeItAndNotTheOnesAfter(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	var before, after Voice
	h.record(func(queue *Queue) {
		before = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(music)})
		queue.StopBus(music)
		after = queue.Play(ClipWithResource(bell), 0, Params{Bus: m.Some(music)})
	})
	h.tick()

	if h.probe(before).Found {
		t.Fatalf("%v was recorded before the StopBus and survived it", before)
	}
	if !h.probe(after).Found {
		t.Fatalf("%v was recorded after the StopBus and did not survive it", after)
	}
	if ended := h.waitEnded(); ended.Voice != before || ended.Reason != ReasonStopped {
		t.Fatalf("%v ended as %v, want %v/stopped", ended.Voice, ended.Reason, before)
	}

	// The stopped Voice was never audible, so it has no start to emit and no
	// stop to pair with one: the tick applied atomically, as it always does.
	batch := h.backend.emitted()[0]
	if len(batch.Starts) != 1 || batch.Starts[0].Slot != 1 {
		t.Fatalf("the tick emitted %+v, want the one start the Play after the StopBus made", batch.Starts)
	}
	if len(batch.Stops) != 0 {
		t.Fatalf("the tick emitted %d stops for a Voice that was never audible", len(batch.Stops))
	}
}
