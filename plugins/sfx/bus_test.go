package sfx_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The buses a game declares. sfx declares only Master, and these two are what
// every settings screen ever built has on it.
const (
	busMusic   sfx.Bus = 1
	busEffects sfx.Bus = 2
)

// playOn records a play of the fixture clip on bus and answers its handle.
func playOn(h *sfxtest.Harness, bus sfx.Bus) sfx.Voice {
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(bus)}) })
	h.Step()
	return voice
}

// readBusVolume reads a bus's volume the way a settings screen does: under a
// read lock on a resource of its own, never by asking the queue it records
// into.
func readBusVolume(h *sfxtest.Harness, bus sfx.Bus) float32 {
	var volume float32
	h.Read(func(_ sfx.LiveVoices, _ sfx.ClipTable, buses sfx.BusVolumes, _ sfx.Listener, _ sfx.DeviceView) {
		volume = buses.Volume(bus)
	})
	return volume
}

// stereoGains answers the gain matrix of the stereo fixture heard centred at
// audibility: its own diagonal, scaled, which isSameMatrix compares within
// gainTolerance.
func stereoGains(audibility float32) [2][2]float32 {
	return [2][2]float32{{audibility, 0}, {0, audibility}}
}

// The whole of what a bus is for: a settings screen moves a slider and the
// music gets quieter while the footsteps do not.
//
// The fold happens above the seam, so it is visible twice: in the audibility
// the view reports, which is what a game's test asserts on, and in the gain
// matrix the backend is handed, which carries no bus of any kind.
func TestABusVolumeFallsOnItsOwnVoicesAndNoOthers(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	var track, footsteps sfx.Voice
	h.Record(func(queue sfx.Queue) {
		track = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic)})
		footsteps = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busEffects)})
	})
	h.Step()

	if entry, _ := readLiveVoice(h, track); entry.Audibility != 1 || entry.Bus != busMusic {
		t.Fatalf("the music is on bus %d at %v before any slider moved, want bus %d at unity", entry.Bus, entry.Audibility, busMusic)
	}

	h.Record(func(queue sfx.Queue) { queue.SetBus(busMusic, 0.5) })
	h.Step()

	if entry, _ := readLiveVoice(h, track); entry.Audibility != 0.5 {
		t.Fatalf("the music is audible at %v on a bus at half, want 0.5", entry.Audibility)
	}
	if entry, _ := readLiveVoice(h, footsteps); entry.Audibility != 1 {
		t.Fatalf("the footsteps are audible at %v, and nothing touched their bus, want 1", entry.Audibility)
	}

	// The bus volume change re-emits every voice on that bus and no other, and
	// what it re-emits is a gain matrix: a backend does not know buses exist.
	changes := seam.emitted()[1]
	if len(changes.Updates) != 1 || changes.Updates[0].Slot != 0 {
		t.Fatalf("the slider produced the updates %+v, want one for the music's slot 0", changes.Updates)
	}
	if got := changes.Updates[0].Params.Gains; !isSameMatrix(got, stereoGains(0.5)) {
		t.Fatalf("the update carries gains %v, want the bus folded in at 0.5", got)
	}
}

// A bus volume is readable, so a settings screen draws its slider where the
// player left it, and every bus starts at unity, so a game that declares none
// is audible and its master slider works.
//
// It is coalesced within the flush, last value winning: a slider dragged
// through a hundred values before one flush is one value by the time anything
// is folded with it, and the voices on it are re-emitted once.
func TestABusVolumeIsReadableAndCoalescedWithinOneFlush(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	if got := readBusVolume(h, busMusic); got != 1 {
		t.Fatalf("a bus nobody has set is at %v, want unity", got)
	}

	voice := playOn(h, busMusic)

	h.Record(func(queue sfx.Queue) {
		for _, dragged := range []float32{0.9, 0.7, 0.3} {
			queue.SetBus(busMusic, dragged)
		}
	})
	h.Step()

	if got := readBusVolume(h, busMusic); got != 0.3 {
		t.Fatalf("the bus reads back at %v, want the last value the flush recorded", got)
	}
	if entry, _ := readLiveVoice(h, voice); entry.Audibility != 0.3 {
		t.Fatalf("the voice is audible at %v, want the last value the flush recorded", entry.Audibility)
	}
	if got := seam.emitted()[1].Updates; len(got) != 1 || !isSameMatrix(got[0].Params.Gains, stereoGains(0.3)) {
		t.Fatalf("three values in one flush produced the updates %+v, want one at 0.3", got)
	}

	// A volume that did not move is not a change, so nothing is re-emitted for
	// it: the cost is bounded by MaxVoices per change, not paid every flush.
	h.Record(func(queue sfx.Queue) { queue.SetBus(busMusic, 0.3) })
	h.Step()
	if got := seam.emitted()[2].Updates; len(got) != 0 {
		t.Fatalf("setting a bus to the volume it already had produced %d updates", len(got))
	}
}

// A voice played onto a bus already moved starts at that bus's volume: the
// fold reaches its start, so no update is owed beside it.
func TestAVoiceStartsAtItsBusVolume(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	h.Record(func(queue sfx.Queue) { queue.SetBus(busMusic, 0.25) })
	h.Step()
	playOn(h, busMusic)

	changes := seam.emitted()[1]
	if len(changes.Starts) != 1 || !isSameMatrix(changes.Starts[0].Params.Gains, stereoGains(0.25)) || len(changes.Updates) != 0 {
		t.Fatalf("the play emitted the starts %+v and %d updates, want one start at 0.25 and no update", changes.Starts, len(changes.Updates))
	}
}

// An absent bus lands on Master, and so does one outside Config.MaxBuses: a
// mis-declared constant does not silence a game, and the rule is the same
// wherever a bus is named, so a game whose constant is wrong hears exactly
// what it asked for, one bus over.
func TestAnAbsentBusAndAnOutOfRangeOneBothLandOnMaster(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxBuses: 4})

	const misdeclared sfx.Bus = 4 + 7

	var unset, strayed, below sfx.Voice
	h.Record(func(queue sfx.Queue) {
		unset = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		strayed = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(misdeclared)})
		below = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some[sfx.Bus](-1)})
	})
	h.Step()
	onMaster := []sfx.Voice{unset, strayed, below}

	for _, voice := range onMaster {
		if entry, _ := readLiveVoice(h, voice); entry.Bus != sfx.Master {
			t.Fatalf("%v is on bus %d, want Master", voice, entry.Bus)
		}
	}

	// The slider a game wired to its mis-declared constant moves Master, which
	// is the bus its sounds are actually on.
	h.Record(func(queue sfx.Queue) { queue.SetBus(misdeclared, 0.25) })
	h.Step()

	if got := readBusVolume(h, misdeclared); got != 0.25 {
		t.Fatalf("the out-of-range bus reads back at %v, want 0.25", got)
	}
	if got := readBusVolume(h, sfx.Master); got != 0.25 {
		t.Fatalf("Master reads back at %v, want the 0.25 the out-of-range SetBus landed on", got)
	}
	for _, voice := range onMaster {
		if entry, _ := readLiveVoice(h, voice); entry.Audibility != 0.25 {
			t.Fatalf("%v is audible at %v, want Master's 0.25", voice, entry.Audibility)
		}
	}
}

// StopBus ends its voices with ReasonStopped rather than a member of its own:
// it is Stop over a set, and a reader switching on ReasonStopped keeps
// working. Master stops everything, because every bus is directly under it.
func TestStopBusEndsItsVoicesWithReasonStoppedAndMasterStopsEverything(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	var first, second, footsteps sfx.Voice
	h.Record(func(queue sfx.Queue) {
		first = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic)})
		second = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic)})
		footsteps = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busEffects)})
	})
	h.Step()

	h.Record(func(queue sfx.Queue) { queue.StopBus(busMusic) })
	h.Step()

	for _, voice := range []sfx.Voice{first, second} {
		if _, live := readLiveVoice(h, voice); live {
			t.Fatalf("%v survived a StopBus on its own bus", voice)
		}
	}
	if _, live := readLiveVoice(h, footsteps); !live {
		t.Fatalf("%v ended, and the StopBus named another bus", footsteps)
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: first, Reason: sfx.ReasonStopped},
		sfx.VoiceEndedEvent{Voice: second, Reason: sfx.ReasonStopped})
	if got := seam.emitted()[1].Stops; !slices.Equal(got, []sfx.VoiceSlot{0, 1}) {
		t.Fatalf("the StopBus emitted the stops %v, want the two voices on that bus", got)
	}

	h.Record(func(queue sfx.Queue) { queue.StopBus(sfx.Master) })
	h.Step()

	if count := readLiveCount(h); count != 0 {
		t.Fatalf("a StopBus on Master left %d voices, and Master stops everything", count)
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: first, Reason: sfx.ReasonStopped},
		sfx.VoiceEndedEvent{Voice: second, Reason: sfx.ReasonStopped},
		sfx.VoiceEndedEvent{Voice: footsteps, Reason: sfx.ReasonStopped})
}

// StopBus is recorded in order rather than coalesced, and this is the
// sequence that says why: a play recorded before it is stopped, and one
// recorded after it is not. A bus volume has no such sequence, which is why
// that one is a table the flush's last word wins and this one an operation in
// the list.
func TestStopBusStopsThePlaysBeforeItAndNotTheOnesAfter(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	var before, after sfx.Voice
	h.Record(func(queue sfx.Queue) {
		before = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic)})
		queue.StopBus(busMusic)
		after = queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic)})
	})
	h.Step()

	if _, live := readLiveVoice(h, before); live {
		t.Fatalf("%v was recorded before the StopBus and survived it", before)
	}
	if _, live := readLiveVoice(h, after); !live {
		t.Fatalf("%v was recorded after the StopBus and did not survive it", after)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: before, Reason: sfx.ReasonStopped})

	// The stopped voice was never audible, so it has no start to emit and no
	// stop to pair with one: the flush applied atomically, as it always does.
	changes := seam.emitted()[0]
	if len(changes.Starts) != 1 || changes.Starts[0].Slot != 1 {
		t.Fatalf("the flush emitted the starts %+v, want the one the play after the StopBus made", changes.Starts)
	}
	if len(changes.Stops) != 0 {
		t.Fatalf("the flush emitted %d stops for a voice that was never audible", len(changes.Stops))
	}
}

// A negative bus count is a composition error, and panics in Init.
func TestANegativeMaxBusesPanics(t *testing.T) {
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "MaxBuses") {
			t.Fatalf("a negative MaxBuses panicked with %v, want a panic naming MaxBuses", recovered)
		}
	}()
	supervisor.Spawn(map[kernel.PluginName]any{sfx.PluginName: &sfx.Config{MaxBuses: -1}}, 1, platform.NewPlugin(), sfx.NewPlugin())
}

// Config.MaxBuses of zero means 32, Master among them: the last bus in range
// is a bus of its own, and the one past it is Master.
func TestAZeroMaxBusesMeansThirtyTwo(t *testing.T) {
	h := newHarness(t, nil)

	h.Record(func(queue sfx.Queue) {
		queue.SetBus(31, 0.5)
		queue.SetBus(32, 0.25)
	})
	h.Step()

	if got := readBusVolume(h, 31); got != 0.5 {
		t.Fatalf("bus 31 reads back at %v, want its own 0.5", got)
	}
	if got := readBusVolume(h, sfx.Master); got != 0.25 {
		t.Fatalf("Master reads back at %v, want the 0.25 bus 32 landed on", got)
	}
}
