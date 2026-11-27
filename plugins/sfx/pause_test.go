package sfx_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// setPaused records the pause of every voice, or its end, and steps.
func setPaused(h *sfxtest.Harness, paused bool) {
	h.Record(func(queue sfx.Queue) { queue.SetPaused(paused) })
	h.Step()
}

// wantOnlyUpdates fails the test unless changes holds no start and no stop,
// and an update of each slot in slots, in order, every one carrying paused.
func wantOnlyUpdates(t *testing.T, changes sfx.FlushChanges, paused bool, slots ...sfx.VoiceSlot) {
	t.Helper()
	if len(changes.Starts) != 0 || len(changes.Stops) != 0 {
		t.Fatalf("a pause changed %d starts and %d stops, and it suspends rather than starting or ending anything", len(changes.Starts), len(changes.Stops))
	}
	var updated []sfx.VoiceSlot
	for _, update := range changes.Updates {
		if update.Params.Paused != paused {
			t.Fatalf("the update of slot %d says Paused %v, want %v", update.Slot, update.Params.Paused, paused)
		}
		updated = append(updated, update.Slot)
	}
	if !slices.Equal(updated, slots) {
		t.Fatalf("the flush updated the slots %v, want %v", updated, slots)
	}
}

// A pause suspends rather than silences: the playhead stops and resumes on
// the same sample, which is why it crosses as VoiceParams.Paused and not as a
// gain of zero. A paused game coming back to its music finds it where it
// left it.
//
// The game keeps stepping here, as it does under its pause menu, and nothing
// advances anyway: a flush that moved a suspended playhead would resume the
// music somewhere the backend is not. The flush that applies the pause
// advances no playhead, and the one that ends it does.
func TestAPauseSuspendsEveryVoiceAndResumesItOnTheSameSample(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	setPaused(h, true)
	wantOnlyUpdates(t, seam.emitted()[1], true, 0)

	h.Step()
	h.Step()
	paused, _ := readLiveVoice(h, voice)
	if !paused.Paused {
		t.Fatal("the view does not say a voice under the pause is paused")
	}
	if paused.Playhead != sfxtest.StepInterval {
		t.Fatalf("a suspended playhead moved to %v from %v", paused.Playhead, sfxtest.StepInterval)
	}

	setPaused(h, false)
	wantOnlyUpdates(t, seam.emitted()[4], false, 0)
	resumed, _ := readLiveVoice(h, voice)
	if resumed.Paused {
		t.Fatal("the view still says the voice is paused after the pause ended")
	}
	if resumed.Playhead != 2*sfxtest.StepInterval {
		t.Fatalf("the resumed playhead is %v, want %v", resumed.Playhead, 2*sfxtest.StepInterval)
	}
}

// The pause is one update per live voice, at most MaxVoices of them, and no
// verb: a SuspendAll at the seam would be a second way to say what the
// flush's changes already say, and two ways to say one thing can disagree.
//
// A voice's own VoiceOptions.Paused is what the game itself said, and it
// survives the pause untouched: ending the pause must not unpause the voice a
// game paused.
func TestAPauseIsAnUpdatePerLiveVoiceAndLeavesWhatTheGameSaid(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, &sfx.Config{MaxVoices: 4}, seamPlugin{seam: seam})

	var voices []sfx.Voice
	h.Record(func(queue sfx.Queue) {
		voices = append(voices, queue.Play(clipPath, 0, sfx.VoiceOptions{Paused: m.Some(true)}))
		for range 2 {
			voices = append(voices, queue.Play(clipPath, 0, sfx.VoiceOptions{}))
		}
	})
	h.Step()

	setPaused(h, true)
	wantOnlyUpdates(t, seam.emitted()[1], true, 0, 1, 2)

	setPaused(h, false)
	changes := seam.emitted()[2]
	if len(changes.Updates) != len(voices) || !changes.Updates[0].Params.Paused || changes.Updates[1].Params.Paused {
		t.Fatalf("the end of the pause updated %+v, want every live voice, the game's own paused one still paused", changes.Updates)
	}
	own, _ := readLiveVoice(h, voices[0])
	if !own.Paused || own.Playhead != 0 {
		t.Fatalf("the voice the game paused itself reads %+v after the pause came and went, want it paused at the head", own)
	}
	if other, _ := readLiveVoice(h, voices[1]); other.Paused {
		t.Fatal("a voice nothing paused is still paused after the pause ended")
	}
}

// The pause beats a device that is not ready. A pause while the device is not
// ready suspends everything, the playhead rule included, which is the one
// place the rule that a playhead advances whether or not anyone can hear it
// does not hold.
func TestAPauseBeatsADeviceThatIsNotReady(t *testing.T) {
	h := newHarness(t, nil, seamPlugin{seam: &seam{device: m.Some(sfx.Device{Name: "notready"})}})

	voice := play(h)
	setPaused(h, true)
	for range 4 {
		h.Step()
	}

	if got, _ := readLiveVoice(h, voice); got.Playhead != sfxtest.StepInterval {
		t.Fatalf("a playhead moved to %v under a pause with no device, want %v", got.Playhead, sfxtest.StepInterval)
	}
}

// A voice played during a pause starts suspended, from its first flush,
// without anybody having said so about it, and plays once the pause ends.
func TestAVoicePlayedDuringAPauseStartsSuspended(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	setPaused(h, true)
	voice := play(h)
	h.Step()

	if starts := seam.emitted()[1].Starts; len(starts) != 1 || !starts[0].Params.Paused {
		t.Fatalf("the play during the pause started %+v, want one start carrying Paused", starts)
	}
	if got, _ := readLiveVoice(h, voice); !got.Paused || got.Playhead != 0 {
		t.Fatalf("the voice played during the pause reads %+v, want it paused at the head", got)
	}

	setPaused(h, false)
	wantOnlyUpdates(t, seam.emitted()[3], false, 0)
	if got, _ := readLiveVoice(h, voice); got.Paused || got.Playhead != sfxtest.StepInterval {
		t.Fatalf("after the pause the voice reads %+v, want it playing one step in", got)
	}
}

// The pause is coalesced as a bus's volume and the listener are: set twice
// before one flush, the last value wins, and a pause set to what it already
// is changes nothing at the seam.
func TestThePauseSetLastBeforeAFlushWins(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	h.Record(func(queue sfx.Queue) {
		queue.SetPaused(true)
		queue.SetPaused(false)
	})
	h.Step()
	wantOnlyUpdates(t, seam.emitted()[1], false)
	if got, _ := readLiveVoice(h, voice); got.Paused || got.Playhead != 2*sfxtest.StepInterval {
		t.Fatalf("a pause ended before its flush left the voice at %+v, want it playing two steps in", got)
	}

	h.Record(func(queue sfx.Queue) {
		queue.SetPaused(false)
		queue.SetPaused(true)
	})
	h.Step()
	wantOnlyUpdates(t, seam.emitted()[2], true, 0)

	setPaused(h, true)
	wantOnlyUpdates(t, seam.emitted()[3], true)
	if got, _ := readLiveVoice(h, voice); !got.Paused || got.Playhead != 2*sfxtest.StepInterval {
		t.Fatalf("the voice reads %+v under the pause, want it paused two steps in", got)
	}
}

// Stop still applies during a pause: the stopped voice ends as stopped and
// crosses the seam as an ordinary stop.
func TestAStopStillAppliesDuringAPause(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	setPaused(h, true)
	h.Record(func(queue sfx.Queue) { queue.Stop(voice) })
	h.Step()

	if changes := seam.emitted()[2]; !slices.Equal(changes.Stops, []sfx.VoiceSlot{0}) || len(changes.Updates) != 0 {
		t.Fatalf("a stop during the pause emitted %+v, want slot 0's stop alone", changes)
	}
	if _, live := readLiveVoice(h, voice); live {
		t.Fatalf("%v outlived its stop during the pause", voice)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})
}
