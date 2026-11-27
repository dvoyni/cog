package sfx_test

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The view carries what the game commanded and what sfx derived from it,
// which is what a game's test asserts on: the options as the game said them,
// the bus they resolved to, and the audibility they come to.
func TestTheViewCarriesWhatTheGameCommandedAndWhatSfxDerived(t *testing.T) {
	h := newHarness(t, nil)

	options := sfx.VoiceOptions{Bus: m.Some[sfx.Bus](3), Volume: m.Some[float32](0.4)}
	voice := playWith(h, options)

	entry, live := readLiveVoice(h, voice)
	if !live {
		t.Fatal("the voice is not in the view")
	}
	want := sfx.VoiceEntry{
		Voice:      voice,
		Path:       clipPath,
		Bus:        3,
		Playhead:   sfxtest.StepInterval,
		Duration:   clipDuration,
		Options:    options,
		Audibility: 0.4,
	}
	if entry != want {
		t.Fatalf("the view holds %+v, want %+v", entry, want)
	}
}

// A voice's own pause suspends rather than silences: the playhead stops and
// resumes on the same sample. A paused game that comes back to its music
// thirty seconds in is a bug in every game that has ever paused.
func TestAPausedVoiceSuspendsAndResumesWhereItStopped(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Paused: m.Some(true)}) })
	h.Step()
	h.Step()

	paused, _ := readLiveVoice(h, voice)
	if !paused.Paused || paused.Options.Paused != m.Some(true) {
		t.Fatalf("the view reads %+v, want the voice paused by its own options", paused)
	}
	if paused.Playhead != sfxtest.StepInterval {
		t.Fatalf("a paused playhead moved to %v from %v", paused.Playhead, sfxtest.StepInterval)
	}
	wantOnlyUpdates(t, seam.emitted()[1], true, 0)

	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Paused: m.Some(false)}) })
	h.Step()

	wantOnlyUpdates(t, seam.emitted()[3], false, 0)
	if resumed, _ := readLiveVoice(h, voice); resumed.Playhead != 2*sfxtest.StepInterval {
		t.Fatalf("the resumed playhead is %v, want %v", resumed.Playhead, 2*sfxtest.StepInterval)
	}
}

// A Play followed by a SetVoice in one flush is indistinguishable from a
// Play that carried the same options: the flush's operations apply in the
// order they were recorded, and what crosses the seam is where they left the
// voice, one start and no update beside it.
func TestAPlayAndASetVoiceInOneFlushReachTheSeamAsOne(t *testing.T) {
	setSeam := &seam{}
	set := newHarness(t, nil, seamPlugin{seam: setSeam})
	set.Record(func(queue sfx.Queue) {
		voice := queue.Play(clipPath, 0, sfx.VoiceOptions{Volume: m.Some[float32](1)})
		queue.SetVoice(voice, sfx.VoiceOptions{Volume: m.Some[float32](0.25)})
	})
	set.Step()

	carriedSeam := &seam{}
	carried := newHarness(t, nil, seamPlugin{seam: carriedSeam})
	playWith(carried, sfx.VoiceOptions{Volume: m.Some[float32](0.25)})

	changes := setSeam.emitted()[0]
	if len(changes.Starts) != 1 || len(changes.Updates) != 0 {
		t.Fatalf("the flush emitted %d starts and %d updates, want the one start the SetVoice folded into", len(changes.Starts), len(changes.Updates))
	}
	if got := changes.Starts[0].Params.Gains; !isSameMatrix(got, stereoGains(0.25)) {
		t.Fatalf("the start carries gains %v, want the SetVoice's 0.25 on the diagonal", got)
	}
	if got, want := changes.Starts[0], carriedSeam.emitted()[0].Starts[0]; got != want {
		t.Fatalf("a Play and a SetVoice started %+v, and a Play carrying the options starts %+v", got, want)
	}
}

// A SetVoice naming a bus moves the voice onto it, its volume folded with
// that bus's from the flush that applies it, and a bus outside
// Config.MaxBuses is Master, as at Play.
func TestASetVoiceMovesAVoiceOntoAnotherBus(t *testing.T) {
	h := newHarness(t, nil)

	h.Record(func(queue sfx.Queue) { queue.SetBus(2, 0.5) })
	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Bus: m.Some[sfx.Bus](2)}) })
	h.Step()
	if got, _ := readLiveVoice(h, voice); got.Bus != 2 || !isClose(got.Audibility, 0.5) {
		t.Fatalf("the voice moved onto bus 2 reads bus %d at %v, want bus 2 at its 0.5", got.Bus, got.Audibility)
	}

	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Bus: m.Some[sfx.Bus](99)}) })
	h.Step()
	if got, _ := readLiveVoice(h, voice); got.Bus != sfx.Master || !isClose(got.Audibility, 1) {
		t.Fatalf("the voice moved onto bus 99 reads bus %d at %v, want Master at unity", got.Bus, got.Audibility)
	}
}

// Pitch is the backend's Rate, and sfx scales its own playhead by it. It has
// to: sfx computes the ending from the duration and the rate, and a voice at
// twice its rate whose playhead crawled at one would run out in the mixer
// half a clip before sfx said so, and be cut there rather than stopped here.
func TestPitchCrossesTheSeamAsRateAndScalesThePlayhead(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	// Two steps of the clip a flush: the fixture's 70.68 steps take 35.34
	// flushes, so the voice is playing after 35 and gone after 36.
	const endsOnFlush = 36
	voice := playWith(h, sfx.VoiceOptions{Pitch: m.Some[float32](2)})
	for flush := 2; flush < endsOnFlush; flush++ {
		h.Step()
	}
	if got, live := readLiveVoice(h, voice); !live || got.Playhead != 2*(endsOnFlush-1)*sfxtest.StepInterval {
		t.Fatalf("after %d flushes at twice the rate the voice reads %+v (live %v), want it %v in", endsOnFlush-1, got, live, 2*(endsOnFlush-1)*sfxtest.StepInterval)
	}
	if got := seam.emitted()[0].Starts[0].Params.Rate; got != 2 {
		t.Fatalf("the start carries a rate of %v, want 2", got)
	}
	wantEndings(t, h)

	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
}

// A negative pitch would run a voice backwards off the front of its clip, so
// it is a rate of zero, a legitimate freeze: the playhead stays where it is.
func TestANegativePitchIsAFreeze(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := playWith(h, sfx.VoiceOptions{Pitch: m.Some[float32](-1)})
	h.Step()

	if got, _ := readLiveVoice(h, voice); got.Playhead != 0 {
		t.Fatalf("a voice at a negative pitch moved to %v", got.Playhead)
	}
	if got := seam.emitted()[0].Starts[0].Params.Rate; got != 0 {
		t.Fatalf("the start carries a rate of %v, want 0", got)
	}
}

// A looping voice never ends by itself: it publishes nothing until a stop, a
// steal, a failure or a release reaches it. Loop reaches the backend on the
// start, and a seek past the end wraps to the loop start, which on a clip
// that declares no region is its head.
func TestALoopingVoiceWrapsAndNeverEndsByItself(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	// A hundred flushes is one whole pass of the fixture and most of a
	// second.
	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	for range 99 {
		h.Step()
	}
	wantEndings(t, h)

	got, live := readLiveVoice(h, voice)
	if !live {
		t.Fatalf("%v ended by itself, and a looping voice never does", voice)
	}
	if want := 100*sfxtest.StepInterval - clipDuration; got.Playhead != want {
		t.Fatalf("the looping playhead is %v after a hundred flushes, want %v", got.Playhead, want)
	}
	if start := seam.emitted()[0].Starts[0]; !start.Loop {
		t.Fatal("the start that crossed the seam does not loop")
	}

	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 9*time.Second) })
	h.Step()
	if got, live := readLiveVoice(h, voice); !live || got.Playhead != sfxtest.StepInterval {
		t.Fatalf("a seek past the end of a looping voice left it at %v (live %v), want the loop start and a step", got.Playhead, live)
	}
}

// Loop is a fact of a VoiceStart at the seam and VoiceUpdate has no field for
// it, so a game that changes its mind about looping restarts the voice where
// it stands. The alternative is sfx's playhead wrapping while the backend's
// does not: a voice that goes silent while the view insists it is playing.
func TestChangingLoopRestartsTheVoiceWhereItStands(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	for range 3 {
		h.Step()
	}

	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Loop: m.Some(true)}) })
	h.Step()

	changes := seam.emitted()[4]
	if len(changes.Starts) != 1 || len(changes.Updates) != 0 {
		t.Fatalf("changing Loop emitted %+v, want the one start the seam can say it with", changes)
	}
	if !changes.Starts[0].Loop {
		t.Fatal("the restart does not carry the Loop the game just asked for")
	}
	if got := changes.Starts[0].Offset; got != 4*sfxtest.StepInterval {
		t.Fatalf("the restart begins at %v, want where the voice stood at %v", got, 4*sfxtest.StepInterval)
	}
	if got, _ := readLiveVoice(h, voice); got.Playhead != 5*sfxtest.StepInterval {
		t.Fatalf("the restarted playhead is %v, want %v", got.Playhead, 5*sfxtest.StepInterval)
	}
}
