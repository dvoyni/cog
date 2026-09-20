package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// A long Clip, so nothing in these tests ends by running out: the only thing
// that ends a Voice here is the cap.
func longClip() fakeClip { return fakeClip{duration: 60, channels: 2, rate: 48000} }

// footstep and music are the two things every stealing rule is stated about.
// Priority is the band, and the numbers below it are what the band beats.
func footstep(volume float32) sound.Params {
	return sound.Params{Volume: m.Some(volume), Priority: m.Some(0)}
}

func track(volume float32) sound.Params {
	return sound.Params{Volume: m.Some(volume), Priority: m.Some(1)}
}

// drainEndings takes exactly n endings, or fails. Every ending a flush collects
// is published after the batch has been handed over, so a test that expects
// several waits for all of them before it asserts about any.
func (h *harness) drainEndings(n int) map[sound.Voice]sound.Reason {
	h.t.Helper()
	drained := make(map[sound.Voice]sound.Reason, n)
	for range n {
		event := h.waitEnded()
		drained[event.Voice] = event.Reason
	}
	h.noEnding()
	return drained
}

// A hundred Entities emitting on one frame costs the cap and the music
// survives, which is the whole reason the cap exists. The footsteps take the
// slots the footsteps had, and the ones that find nothing left to outrank lose
// outright rather than reaching up a band for the music.
//
// The arithmetic, at four slots: three old footsteps are outranked and stolen,
// and the seven plays after them find only the music left, which they cannot
// take. Every one of the ten is answered in the flush that recorded it.
func TestACrowdOfFootstepsCostsTheCapAndTheMusicSurvives(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(4), clipBytes)

	var music sound.Voice
	var old [3]sound.Voice
	h.record(func(queue *sound.Queue) {
		music = queue.Play(sound.ClipWithResource(bell), 0, track(0.5))
		for i := range old {
			old[i] = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
		}
	})
	h.tick()
	if got := h.probe(music); !got.Found || got.Live != 4 {
		t.Fatalf("the table holds %d voices before the crowd, want the four it was given", got.Live)
	}

	crowd := make([]sound.Voice, 10)
	h.record(func(queue *sound.Queue) {
		for i := range crowd {
			crowd[i] = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
		}
	})
	h.tick()

	ended := h.drainEndings(10)
	if got := h.probe(music); !got.Found {
		t.Fatal("the music was stolen by a crowd of footsteps a band below it")
	} else if got.Live != 4 {
		t.Fatalf("the table holds %d voices after the crowd, want the cap of four", got.Live)
	}
	for _, voice := range old {
		if ended[voice] != sound.ReasonStolen {
			t.Fatalf("the footstep %v ended as %v, want stolen", voice, ended[voice])
		}
	}
	lost := 0
	for _, voice := range crowd {
		switch reason, found := ended[voice]; {
		case !found:
		case reason != sound.ReasonStolen:
			t.Fatalf("the incoming play %v ended as %v, want stolen", voice, reason)
		default:
			lost++
		}
	}
	if lost != 7 {
		t.Fatalf("%d of the crowd lost the cap, want the seven that found only the music", lost)
	}
}

// The tie-break is contract. The same Clip played twice in one tick at one
// position ties on both terms exactly - a double footstep, two shell casings, a
// burst weapon - and the older of the two is the one that loses. Left to
// whatever order the table happened to be walked in, this is the test that goes
// flaky on a schedule nobody controls.
func TestATieOnBothTermsBreaksByAgeOldestFirst(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(3), clipBytes)

	// One position, one Clip, one set of Params: the two footsteps differ in
	// nothing a rank can read except when they started.
	same := footstep(1)
	same.Position = m.Some(m.Vec3{X: 3})

	var music, older, younger sound.Voice
	h.record(func(queue *sound.Queue) {
		music = queue.Play(sound.ClipWithResource(bell), 0, track(1))
		older = queue.Play(sound.ClipWithResource(bell), 0, same)
		younger = queue.Play(sound.ClipWithResource(bell), 0, same)
	})
	h.tick()

	first, second := h.probe(older), h.probe(younger)
	if !first.Found || !second.Found {
		t.Fatal("one of the two footsteps did not get a slot")
	}
	if first.Info.Audibility != second.Info.Audibility {
		t.Fatalf("the two footsteps are audible at %v and %v, and this test needs them tied",
			first.Info.Audibility, second.Info.Audibility)
	}

	h.record(func(queue *sound.Queue) { queue.Play(sound.ClipWithResource(bell), 0, same) })
	h.tick()

	ended := h.waitEnded()
	if ended.Voice != older || ended.Reason != sound.ReasonStolen {
		t.Fatalf("the tie ended %v/%v, want the older footstep %v stolen", ended.Voice, ended.Reason, older)
	}
	if got := h.probe(younger); !got.Found {
		t.Fatal("the younger of two tied footsteps lost its slot")
	}
	if got := h.probe(music); !got.Found {
		t.Fatal("the music lost its slot to a footstep")
	}
}

// Priority is a band and not a weight. The music here is the quietest thing on
// the table by two orders of magnitude and is still not the Voice that loses:
// the loud footstep beside it is, because it is a band below.
func TestPriorityIsABandAndNotAWeight(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(2), clipBytes)

	var music, loud sound.Voice
	h.record(func(queue *sound.Queue) {
		music = queue.Play(sound.ClipWithResource(bell), 0, track(0.01))
		loud = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
	})
	h.tick()

	h.record(func(queue *sound.Queue) { queue.Play(sound.ClipWithResource(bell), 0, footstep(1)) })
	h.tick()

	if ended := h.waitEnded(); ended.Voice != loud || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended %v/%v, want the loud footstep %v stolen", ended.Voice, ended.Reason, loud)
	}
	got := h.probe(music)
	if !got.Found {
		t.Fatal("the quietest Voice on the table was stolen, and it was a band above")
	}
	if want := float32(0.01); got.Info.Audibility != want {
		t.Fatalf("the music is audible at %v, want %v - the band, not the gain, saved it",
			got.Info.Audibility, want)
	}
}

// A paused Voice ranks at the audibility it would have if it were not paused.
// Ranking it at zero would make "pause the music for a cutscene" a reliable way
// to lose the music; ranking it first would make pausing a shield.
func TestAPausedVoiceRanksAtTheAudibilityItWouldHave(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(2), clipBytes)

	var loud, quiet sound.Voice
	h.record(func(queue *sound.Queue) {
		loud = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
		quiet = queue.Play(sound.ClipWithResource(bell), 0, footstep(0.2))
	})
	h.tick()

	h.record(func(queue *sound.Queue) {
		queue.SetVoice(loud, sound.Params{Paused: m.Some(true)})
	})
	h.tick()
	if got := h.probe(loud); !got.Info.Paused {
		t.Fatal("the Voice the test paused is not paused")
	}

	h.record(func(queue *sound.Queue) { queue.Play(sound.ClipWithResource(bell), 0, footstep(0.5)) })
	h.tick()

	if ended := h.waitEnded(); ended.Voice != quiet || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended %v/%v, want the quiet Voice %v stolen rather than the paused one",
			ended.Voice, ended.Reason, quiet)
	}
	if got := h.probe(loud); !got.Found {
		t.Fatal("pausing a Voice put it first on the block")
	}
}

// A Voice waiting on its Clip occupies a slot, because its clock is already
// running. A cold-started game hits the cap with nothing audible yet, and that
// is correct: those Voices are going to be audible.
func TestAVoiceWaitingOnItsClipOccupiesASlot(t *testing.T) {
	backend := newFakeBackend(longClip())
	backend.deferring = true
	h := newHarness(t, backend, sound.Config{}.WithMaxVoices(1), clipBytes)

	waiting := h.play(sound.ClipWithResource(bell), 0, track(1))
	h.tick()
	if got := h.probe(waiting); !got.Found || got.Live != 1 || got.Info.Duration != 0 {
		t.Fatalf("a Voice waiting on its Clip reads %+v, want one live Voice with no duration yet", got.Info)
	}

	late := h.play(sound.ClipWithResource(bell), 0, footstep(1))
	h.tick()

	if ended := h.waitEnded(); ended.Voice != late || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended %v/%v, want the incoming play %v stolen", ended.Voice, ended.Reason, late)
	}
	if got := h.probe(waiting); !got.Found || got.Live != 1 {
		t.Fatal("the Voice waiting on its Clip lost the slot its clock was already running in")
	}
}

// Priority is a per-Voice field and rides the one ordered list of operations
// like every other one: a SetVoice raising it is what a game does when a sound
// becomes important after it started, and the band it moves into is the band it
// is ranked in from the next tick on.
func TestPriorityIsRestatedByTheSameOrderedListEveryOtherParamIs(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(2), clipBytes)

	var promoted, ordinary sound.Voice
	h.record(func(queue *sound.Queue) {
		promoted = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
		ordinary = queue.Play(sound.ClipWithResource(bell), 0, footstep(1))
	})
	h.tick()

	h.record(func(queue *sound.Queue) {
		queue.SetVoice(promoted, sound.Params{Priority: m.Some(1)})
	})
	h.tick()
	if got, _ := h.probe(promoted).Info.Params.Priority.Get(); got != 1 {
		t.Fatalf("the view reports priority %d after the SetVoice, want 1", got)
	}

	h.record(func(queue *sound.Queue) { queue.Play(sound.ClipWithResource(bell), 0, footstep(1)) })
	h.tick()

	if ended := h.waitEnded(); ended.Voice != ordinary || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended %v/%v, want the Voice still in the lower band %v stolen",
			ended.Voice, ended.Reason, ordinary)
	}
	if got := h.probe(promoted); !got.Found {
		t.Fatal("a Voice promoted a band by SetVoice was stolen by the band below it")
	}
}

// A stolen Voice reaches the Adapter as an ordinary stop, ordered before the
// start that takes its slot over. Nothing about priorities, ranks or stealing
// crosses the seam at all - VoiceParams is a gain matrix, a rate and a paused
// flag, and there is nowhere for a band to be written down.
func TestAStolenVoiceReachesTheAdapterAsAnOrdinaryStop(t *testing.T) {
	h := newHarness(t, newFakeBackend(longClip()), sound.Config{}.WithMaxVoices(1), clipBytes)

	stolen := h.play(sound.ClipWithResource(bell), 0, footstep(1))
	h.tick()
	if got := h.backend.emitted()[0]; len(got.Starts) != 1 || got.Starts[0].Slot != 0 {
		t.Fatalf("the first tick emitted %+v, want one start on slot 0", got.Starts)
	}

	thief := h.play(sound.ClipWithResource(bell), 0, track(1))
	h.tick()

	if ended := h.waitEnded(); ended.Voice != stolen || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended %v/%v, want %v stolen", ended.Voice, ended.Reason, stolen)
	}
	batch := h.backend.emitted()[1]
	if len(batch.Stops) != 1 || batch.Stops[0] != 0 {
		t.Fatalf("the stealing tick emitted %v stops, want the one ordinary stop slot 0 is owed", batch.Stops)
	}
	if len(batch.Starts) != 1 || batch.Starts[0].Slot != 0 {
		t.Fatalf("the stealing tick emitted %+v, want the one start that reuses slot 0", batch.Starts)
	}
	if got := h.probe(thief); !got.Found || got.Info.Voice != thief {
		t.Fatalf("the Voice that stole slot 0 is not in the view")
	}
}
