package sfx_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// footstep and track are the two things every stealing rule is stated about:
// a footstep in the bottom band and a music track one band up, each on a bus
// whose volume the test sets, which is how loud each is. Priority is the
// band, and the audibility below it is what the band beats.
func footstep(bus sfx.Bus) sfx.VoiceOptions {
	return sfx.VoiceOptions{Bus: m.Some(bus), Priority: m.Some(0)}
}

func track(bus sfx.Bus) sfx.VoiceOptions {
	return sfx.VoiceOptions{Bus: m.Some(bus), Priority: m.Some(1)}
}

// wantStolen fails the test unless endings are exactly voices ending with
// ReasonStolen, in order.
func wantStolen(t *testing.T, endings []sfx.VoiceEndedEvent, voices ...sfx.Voice) {
	t.Helper()
	want := make([]sfx.VoiceEndedEvent, len(voices))
	for i, voice := range voices {
		want[i] = sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStolen}
	}
	if !slices.Equal(endings, want) {
		t.Fatalf("the endings are %v, want %v", endings, want)
	}
}

// A crowd of plays arriving at once costs the cap and the music survives,
// which is the whole reason the cap exists. The footsteps take the slots the
// footsteps had, and the ones that find nothing left to outrank lose outright
// rather than reaching up a band for the music.
//
// The arithmetic, at four slots: three old footsteps are outranked, each tie
// stealing the oldest, and the seven plays after them find only the music
// left, which they cannot take. Every one of the ten is answered in the flush
// that applies it.
func TestACrowdOfFootstepsCostsTheCapAndTheMusicSurvives(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 4})

	var music sfx.Voice
	var old [3]sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetBus(busMusic, 0.5)
		music = queue.Play(clipPath, 0, track(busMusic))
		for i := range old {
			old[i] = queue.Play(clipPath, 0, footstep(busEffects))
		}
	})
	h.Step()
	if count := readLiveCount(h); count != 4 {
		t.Fatalf("the table holds %d voices before the crowd, want the four it was given", count)
	}

	crowd := make([]sfx.Voice, 10)
	h.Record(func(queue sfx.Queue) {
		for i := range crowd {
			crowd[i] = queue.Play(clipPath, 0, footstep(busEffects))
		}
	})
	h.Step()

	wantStolen(t, h.Endings(), append(old[:], crowd[3:]...)...)
	if _, live := readLiveVoice(h, music); !live {
		t.Fatal("the music was stolen by a crowd of footsteps a band below it")
	}
	for _, voice := range crowd[:3] {
		if _, live := readLiveVoice(h, voice); !live {
			t.Fatalf("the footstep %v, which outranked an older one, is not in the view", voice)
		}
	}
	if count := readLiveCount(h); count != 4 {
		t.Fatalf("the table holds %d voices after the crowd, want the cap of four", count)
	}
}

// The tie-break is contract. The same clip played twice before one flush at
// one position ties on both terms exactly, a double footstep, two shell
// casings, a burst weapon, and the older of the two is the one that loses.
// Left to whatever order the table happened to be walked in, this is the test
// that goes flaky on a schedule nobody controls.
func TestATieOnBothTermsBreaksByAgeOldestFirst(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 3})

	// One position, one clip, one set of options: the two footsteps differ in
	// nothing a rank can read except when they started.
	same := footstep(sfx.Master)
	same.Position = m.Some(m.Vec3{X: 3})

	var music, older, younger sfx.Voice
	h.Record(func(queue sfx.Queue) {
		music = queue.Play(clipPath, 0, track(sfx.Master))
		older = queue.Play(clipPath, 0, same)
		younger = queue.Play(clipPath, 0, same)
	})
	h.Step()

	first, _ := readLiveVoice(h, older)
	second, _ := readLiveVoice(h, younger)
	if first.Audibility != second.Audibility || first.Audibility == 1 {
		t.Fatalf("the two footsteps are audible at %v and %v, and this test needs them tied below the music", first.Audibility, second.Audibility)
	}

	h.Record(func(queue sfx.Queue) { queue.Play(clipPath, 0, same) })
	h.Step()

	wantStolen(t, h.Endings(), older)
	if _, live := readLiveVoice(h, younger); !live {
		t.Fatal("the younger of two tied footsteps lost its slot")
	}
	if _, live := readLiveVoice(h, music); !live {
		t.Fatal("the music lost its slot to a footstep")
	}
}

// Priority is a band and not a weight. The music here is the quietest thing
// on the table by two orders of magnitude and is still not the voice that
// loses: the loud footstep beside it is, because it is a band below.
func TestPriorityIsABandAndNotAWeight(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 2})

	var music, loud sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetBus(busMusic, 0.01)
		music = queue.Play(clipPath, 0, track(busMusic))
		loud = queue.Play(clipPath, 0, footstep(sfx.Master))
	})
	h.Step()

	h.Record(func(queue sfx.Queue) { queue.Play(clipPath, 0, footstep(sfx.Master)) })
	h.Step()

	wantStolen(t, h.Endings(), loud)
	entry, live := readLiveVoice(h, music)
	if !live {
		t.Fatal("the quietest voice on the table was stolen, and it was a band above")
	}
	if want := float32(0.01); entry.Audibility != want {
		t.Fatalf("the music is audible at %v, want %v: the band, not the gain, saved it", entry.Audibility, want)
	}
}

// A paused voice ranks at the audibility it would have unpaused. Ranking it at
// zero would make "pause the music for a cutscene" a reliable way to lose the
// music; ranking it first would make pausing a shield. The pause of every
// voice is the same, so the incoming play here arrives under one.
func TestAPausedVoiceRanksAtTheAudibilityItWouldHave(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 2})

	paused := footstep(sfx.Master)
	paused.Paused = m.Some(true)
	var loud, quiet sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetBus(busEffects, 0.2)
		queue.SetBus(busMusic, 0.5)
		loud = queue.Play(clipPath, 0, paused)
		quiet = queue.Play(clipPath, 0, footstep(busEffects))
	})
	h.Step()
	if entry, _ := readLiveVoice(h, loud); !entry.Paused || entry.Audibility != 1 {
		t.Fatalf("the voice the test paused reads paused %v at %v, want paused at unity", entry.Paused, entry.Audibility)
	}

	h.Record(func(queue sfx.Queue) { queue.SetPaused(true) })
	h.Step()
	h.Record(func(queue sfx.Queue) { queue.Play(clipPath, 0, footstep(busMusic)) })
	h.Step()

	wantStolen(t, h.Endings(), quiet)
	if _, live := readLiveVoice(h, loud); !live {
		t.Fatal("pausing a voice put it first on the block")
	}
}

// A voice waiting on its clip occupies a slot, because its clock is already
// running. A cold-started game reaches the cap with nothing audible yet, and
// that is correct: those voices are going to be audible.
func TestAVoiceWaitingOnItsClipOccupiesASlot(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	h := sfxtest.NewHarness(t)
	h.UseBackend(backend)
	h.Spawn(&sfx.Config{MaxVoices: 1})
	h.Mount(clipPath, []byte("a clip the fake backend reads"))

	waiting := playWith(h, track(sfx.Master))
	if entry, live := readLiveVoice(h, waiting); !live || entry.Duration != 0 || readLiveCount(h) != 1 {
		t.Fatalf("a voice waiting on its clip reads %+v, live %v, want the one live voice with no duration yet", entry, live)
	}

	late := playWith(h, footstep(sfx.Master))

	wantStolen(t, h.Endings(), late)
	if _, live := readLiveVoice(h, waiting); !live || readLiveCount(h) != 1 {
		t.Fatal("the voice waiting on its clip lost the slot its clock was already running in")
	}
}

// A stolen voice reaches the backend as an ordinary stop, applied before the
// start that takes its slot over. Nothing about priorities, ranks or stealing
// crosses the seam at all: VoiceParams is a gain matrix, a rate and a paused
// flag, and there is nowhere for a band to be written down.
func TestAStolenVoiceReachesTheBackendAsAnOrdinaryStop(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, &sfx.Config{MaxVoices: 1}, seamPlugin{seam: seam})

	stolen := playWith(h, footstep(sfx.Master))
	if starts := seam.emitted()[0].Starts; len(starts) != 1 || starts[0].Slot != 0 {
		t.Fatalf("the first flush started %+v, want one start on slot 0", starts)
	}

	thief := playWith(h, track(sfx.Master))

	wantStolen(t, h.Endings(), stolen)
	changes := seam.emitted()[1]
	if !slices.Equal(changes.Stops, []sfx.VoiceSlot{0}) {
		t.Fatalf("the stealing flush stopped %v, want the one ordinary stop slot 0 is owed", changes.Stops)
	}
	if len(changes.Starts) != 1 || changes.Starts[0].Slot != 0 {
		t.Fatalf("the stealing flush started %+v, want the one start that reuses slot 0", changes.Starts)
	}
	if entry, live := readLiveVoice(h, thief); !live || entry.Voice != thief {
		t.Fatal("the voice that stole slot 0 is not in the view")
	}
}

// Audibility is one scalar, so "quietest" and "furthest from the listener"
// are one policy: of two footsteps in one band, the far one loses, and a play
// arriving further away than everything live is the one that loses, its own
// position ranked against the listener as the last flush left it, here well
// away from the origin.
func TestTheFurthestVoiceIsTheQuietestAndLosesFirst(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 2})
	const listenerX = 100
	at := func(distance float32) sfx.VoiceOptions {
		options := footstep(sfx.Master)
		options.Position = m.Some(m.Vec3{X: listenerX + distance})
		return options
	}

	var near, far sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetListener(sfx.ListenerOptions{Position: m.Some(m.Vec3{X: listenerX})})
		near = queue.Play(clipPath, 0, at(2))
		far = queue.Play(clipPath, 0, at(8))
	})
	h.Step()

	middle := playWith(h, at(4))
	wantStolen(t, h.Endings(), far)

	since := len(h.Endings())
	furthest := playWith(h, at(16))
	wantStolen(t, h.Endings()[since:], furthest)
	for _, voice := range []sfx.Voice{near, middle} {
		if _, live := readLiveVoice(h, voice); !live {
			t.Fatalf("the footstep %v, nearer than the play that arrived, lost its slot", voice)
		}
	}
}

// The ranking context is one moment: every candidate, the incoming play
// included, is ranked against the bus volumes of the end of the last flush.
// A SetBus recorded before the play, in the same flush, ranks nothing until
// that flush has applied it.
func TestAPlayIsRankedAgainstTheBusVolumesOfTheLastFlush(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 1})

	var held sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetBus(busMusic, 0.1)
		held = queue.Play(clipPath, 0, footstep(busEffects))
	})
	h.Step()

	// The incoming play's bus is turned up before it in the same flush, and
	// it is still ranked at 0.1, below the voice it arrives at, and loses.
	var turnedUp sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.SetBus(busMusic, 1)
		turnedUp = queue.Play(clipPath, 0, footstep(busMusic))
	})
	h.Step()
	wantStolen(t, h.Endings(), turnedUp)

	// From here the bus is at unity, and a play on it ties and steals the
	// older voice.
	since := len(h.Endings())
	playWith(h, footstep(busMusic))
	wantStolen(t, h.Endings()[since:], held)
}

// A play can only steal a voice that was live at the end of the last flush: a
// voice stopped before the play in the same flush gives the play its slot and
// ends as stopped, and is stopped at the seam once.
func TestAVictimStoppedBeforeTheStealEndsStoppedAndIsStoppedOnce(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, &sfx.Config{MaxVoices: 1}, seamPlugin{seam: seam})

	held := playWith(h, footstep(sfx.Master))
	var thief sfx.Voice
	h.Record(func(queue sfx.Queue) {
		queue.Stop(held)
		thief = queue.Play(clipPath, 0, footstep(sfx.Master))
	})
	h.Step()

	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: held, Reason: sfx.ReasonStopped})
	changes := seam.emitted()[1]
	if !slices.Equal(changes.Stops, []sfx.VoiceSlot{0}) || len(changes.Starts) != 1 {
		t.Fatalf("the flush stopped %v and started %+v, want slot 0 stopped once and started once", changes.Stops, changes.Starts)
	}
	if _, live := readLiveVoice(h, thief); !live {
		t.Fatal("the play that took the stopped voice's slot is not in the view")
	}
}
