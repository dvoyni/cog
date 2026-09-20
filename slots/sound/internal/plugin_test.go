package internal

import (
	"errors"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// clipBytes is what a Clip's bytes are to the Slot: something the Library reads
// and hands to Prepare, and nothing else. The fixture Backend never looks at
// them, so what they say does not matter - that they are there does.
var clipBytes = fstest.MapFS{"bell.ogg": {Data: []byte("not really ogg, and the fixture does not care")}}

const bell = "bell.ogg"

// The whole vertical path in one test: a game records a Play, gets a Voice, and
// the Voice ends on the tick its duration says it should - not a tick early,
// not a tick late.
//
// Half a second at a sixty-fourth of a second a tick is thirty-two ticks. The
// Voice plays on tick 1, because the read and the prepare both happen inside
// that tick's flush, and the playhead advances in the same flush that created
// it.
func TestAOneShotEndsOnTheTickItsDurationSaysItShould(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 0.5, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})

	for tick := 1; tick < 32; tick++ {
		h.tick()
		if got := h.probe(voice); !got.Found {
			t.Fatalf("%v was gone after tick %d, and half a second is thirty-two ticks", voice, tick)
		}
	}
	h.noEnding()

	h.tick()
	if got := h.probe(voice); got.Found || got.Live != 0 {
		t.Fatalf("after tick 32 the view still holds %v (%d live)", voice, got.Live)
	}
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonFinished {
		t.Fatalf("ended as %v/%v, want %v/finished", ended.Voice, ended.Reason, voice)
	}
}

// The handle is minted when the play is recorded, so it addresses the Voice in
// the tick that recorded it. A play and a stop in one tick is the sharpest case:
// the Stop names a Voice that does not exist yet, and by the time the flush
// applies it, it does.
//
// Nothing crosses the seam, because nothing was ever audible: the tick applies
// atomically, so a Voice that began and ended inside it has no start to emit and
// no stop to pair with one.
func TestAHandleIsUsableInTheTickThatRecordedIt(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	var voice sound.Voice
	h.record(func(queue *sound.Queue) {
		voice = queue.Play(sound.ClipWithResource(bell), 0, sound.Params{})
		queue.Stop(voice)
	})
	if voice == sound.NoVoice {
		t.Fatal("Play handed back NoVoice")
	}
	h.tick()

	if got := h.probe(voice); got.Found || got.Live != 0 {
		t.Fatalf("the view holds %v (%d live) after a play and a stop in one tick", voice, got.Live)
	}
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonStopped {
		t.Fatalf("ended as %v/%v, want %v/stopped", ended.Voice, ended.Reason, voice)
	}
	batch := h.backend.emitted()[0]
	if len(batch.Starts) != 0 || len(batch.Stops) != 0 {
		t.Fatalf("a voice that never sounded crossed the seam: %d starts, %d stops",
			len(batch.Starts), len(batch.Stops))
	}
}

// The surface is total. Every operation is a no-op on a Voice that is gone,
// with no error and no response field to check, which is what a Stop racing a
// Clip that finished a tick ago needs.
func TestEveryOperationIsANoOpOnAVoiceThatIsGone(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 0.5, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.record(func(queue *sound.Queue) { queue.Stop(voice) })
	h.tick()
	if ended := h.waitEnded(); ended.Reason != sound.ReasonStopped {
		t.Fatalf("ended as %v, want stopped", ended.Reason)
	}

	h.record(func(queue *sound.Queue) {
		queue.Stop(voice)
		queue.Stop(sound.NoVoice)
		queue.SetVoice(voice, sound.Params{Volume: m.Some[float32](0.5)})
		queue.SetVoice(sound.NoVoice, sound.Params{Volume: m.Some[float32](0.5)})
	})
	h.tick()

	if got := h.probe(voice); got.Found || got.Live != 0 {
		t.Fatalf("a stale handle addressed something: found=%v live=%d", got.Found, got.Live)
	}
	h.noEnding()
}

// A Clip that cannot be prepared is reported once, is terminal, and ends its
// Voices with ReasonFailed - one or more ticks after the Play, never in the
// same tick. A game cannot write "play it, and if it fails this frame, do X",
// and that is the rule rather than an accident of scheduling.
func TestAFailedClipEndsItsVoicesLaterAndNeverInTheSameTick(t *testing.T) {
	backend := newFakeBackend(fakeClip{})
	backend.prepareErr = errors.New("this clip is not ogg vorbis")

	var mu sync.Mutex
	var reported []error
	h := newHarnessReporting(t, backend, clipBytes, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		reported = append(reported, err)
	})

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tick()
	if got := h.probe(voice); !got.Found {
		t.Fatal("the Voice was gone in the tick that recorded its play")
	}
	h.noEnding()

	h.tick()
	if got := h.probe(voice); got.Found {
		t.Fatal("the Voice outlived the tick after its Clip failed")
	}
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonFailed {
		t.Fatalf("ended as %v/%v, want %v/failed", ended.Voice, ended.Reason, voice)
	}

	// A second play of the same Clip finds a terminal entry: no second read, no
	// second prepare, and no second report.
	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tick()
	h.tick()
	h.waitEnded()

	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 {
		t.Fatalf("the failure was reported %d times, want once: %v", len(reported), reported)
	}
	var failed sound.ErrClipFailed
	if !errors.As(reported[0], &failed) || failed.Clip != bell {
		t.Fatalf("the report does not name the clip: %v", reported[0])
	}
}

// A Play on a Clip that is not resident creates the Voice immediately: silent,
// addressable, and starting from its offset with no catch-up when the Clip
// installs. Catch-up would serve music sync and ruin one-shots, which are the
// overwhelming majority.
func TestAPlayBeforeItsClipIsReadyStartsFromItsOffsetWithNoCatchUp(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 1, rate: 48000})
	backend.deferring = true
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	const offset = 0.25
	voice := h.play(sound.ClipWithResource(bell), offset, sound.Params{})

	// Three ticks of waiting. The Voice exists, holds its slot and is silent.
	for range 3 {
		h.tick()
	}
	waiting := h.probe(voice)
	if !waiting.Found {
		t.Fatal("a Voice waiting on its Clip does not exist")
	}
	if waiting.Info.Duration != 0 {
		t.Fatalf("a pending Voice reports a duration of %v", waiting.Info.Duration)
	}
	if waiting.Info.Playhead != offset {
		t.Fatalf("a pending Voice's playhead is %v, want its offset %v", waiting.Info.Playhead, offset)
	}
	if starts := countStarts(h.backend.emitted()); starts != 0 {
		t.Fatalf("%d starts crossed the seam before the Clip installed", starts)
	}

	backend.finishPrepares()
	h.tick()

	playing := h.probe(voice)
	if want := float32(offset + step); playing.Info.Playhead != want {
		t.Fatalf("the Voice starts at %v, want %v: three ticks of waiting were caught up",
			playing.Info.Playhead, want)
	}
	if playing.Info.Duration != 4 {
		t.Fatalf("the installed Clip reports a duration of %v, want 4", playing.Info.Duration)
	}
	if starts := countStarts(h.backend.emitted()); starts != 1 {
		t.Fatalf("%d starts crossed the seam, want one", starts)
	}
}

// One flush, one Emit, whatever the tick held - and the Adapter is told how many
// slots exist once, before any of them.
func TestTheFlushEmitsOneBatchPerTick(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 0.5, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}.WithMaxVoices(16), clipBytes)

	if slots, calls := backend.slotCount(); slots != 16 || calls != 1 {
		t.Fatalf("the Adapter was told %d slots %d times, want 16 once", slots, calls)
	}

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 32 {
		h.tick()
	}
	h.waitEnded()

	batches := h.backend.emitted()
	if len(batches) != 32 {
		t.Fatalf("32 ticks produced %d batches", len(batches))
	}
	if len(batches[0].Starts) != 1 || batches[0].Starts[0].Slot != 0 || batches[0].Starts[0].Clip == 0 {
		t.Fatalf("the first batch is %+v, want one start in slot 0 on a real Clip", batches[0].Starts)
	}
	for _, batch := range batches[1:31] {
		if len(batch.Starts) != 0 || len(batch.Stops) != 0 {
			t.Fatalf("a quiet tick carried %d starts and %d stops", len(batch.Starts), len(batch.Stops))
		}
	}
	if got := batches[31].Stops; len(got) != 1 || got[0] != 0 {
		t.Fatalf("the ending tick's stops are %v, want slot 0 alone", got)
	}
	if got := h.probe(voice); got.Found {
		t.Fatal("the view still holds the Voice that ended")
	}
}

// The view carries what the game commanded and what the engine derived from it,
// which is what a game's test asserts on.
func TestTheViewCarriesWhatTheGameCommandedAndWhatTheEngineDerived(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 2, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	clip := sound.ClipWithResource(bell)
	voice := h.play(clip, 0, sound.Params{Bus: m.Some[sound.Bus](3), Volume: m.Some[float32](0.4)})
	h.tick()

	got := h.probe(voice)
	if !got.Found {
		t.Fatal("the Voice is not in the view")
	}
	if got.Info.Voice != voice {
		t.Fatalf("the view names %v, want %v", got.Info.Voice, voice)
	}
	if !got.Info.Clip.Equal(clip) {
		t.Fatal("the view names another Clip")
	}
	if got.Info.Bus != 3 {
		t.Fatalf("the view says Bus %d, want 3", got.Info.Bus)
	}
	if volume, ok := got.Info.Params.Volume.Get(); !ok || volume != 0.4 {
		t.Fatalf("the view says volume %v (set %v), want 0.4", volume, ok)
	}
	if got.Info.Duration != 2 {
		t.Fatalf("the view says duration %v, want 2", got.Info.Duration)
	}
	if got.Info.Playhead != step {
		t.Fatalf("the view says playhead %v, want one tick in at %v", got.Info.Playhead, step)
	}
	if got.Info.Paused {
		t.Fatal("the Voice is paused and nothing paused it")
	}
	if len(got.All) != 1 || got.All[0].Voice != voice {
		t.Fatalf("All yielded %d voices, want the one", len(got.All))
	}
}

// Pause suspends rather than silences: the playhead stops and resumes on the
// same sample. A paused game that comes back to its music thirty seconds in is
// a bug in every game that has ever paused.
func TestAPausedVoiceSuspendsAndResumesWhereItStopped(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tick()

	h.record(func(queue *sound.Queue) { queue.SetVoice(voice, sound.Params{Paused: m.Some(true)}) })
	h.tick()
	h.tick()

	paused := h.probe(voice)
	if !paused.Info.Paused {
		t.Fatal("the view does not say the Voice is paused")
	}
	if paused.Info.Playhead != step {
		t.Fatalf("a paused playhead moved to %v from %v", paused.Info.Playhead, step)
	}

	h.record(func(queue *sound.Queue) { queue.SetVoice(voice, sound.Params{Paused: m.Some(false)}) })
	h.tick()

	if got := h.probe(voice); got.Info.Playhead != 2*step {
		t.Fatalf("the resumed playhead is %v, want %v", got.Info.Playhead, 2*step)
	}
}

// A Play followed by a SetVoice in one tick is indistinguishable from a Play
// that carried the same Params: the tick's operations apply in the order they
// were recorded, and what crosses the seam is where they left the Voice.
func TestAPlayAndASetVoiceInOneTickReachTheSeamAsOne(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.record(func(queue *sound.Queue) {
		voice := queue.Play(sound.ClipWithResource(bell), 0, sound.Params{Volume: m.Some[float32](1)})
		queue.SetVoice(voice, sound.Params{Volume: m.Some[float32](0.25)})
	})
	h.tick()

	batch := h.backend.emitted()[0]
	if len(batch.Starts) != 1 {
		t.Fatalf("the tick produced %d starts, want one", len(batch.Starts))
	}
	if len(batch.Updates) != 0 {
		t.Fatalf("the tick produced %d updates beside the start it folded into", len(batch.Updates))
	}
	if got := batch.Starts[0].Params.Gains; got != [2][2]float32{{0.25, 0}, {0, 0.25}} {
		t.Fatalf("the start carries gains %v, want the SetVoice's 0.25 on the diagonal", got)
	}
}

// The incoming play can be the one that loses, and it still gets a real handle
// whose ending arrives in the same flush that recorded it. The alternative - a
// play always steals something - means the hundredth footstep of a bug silences
// the music, which is the failure the cap exists to prevent.
func TestAPlayThatFindsNoSlotEndsInTheFlushThatRecordedIt(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000}),
		sound.Config{}.WithMaxVoices(1), clipBytes)

	var held, overflow sound.Voice
	h.record(func(queue *sound.Queue) {
		held = queue.Play(sound.ClipWithResource(bell), 0, sound.Params{})
		overflow = queue.Play(sound.ClipWithResource(bell), 0, sound.Params{})
	})
	if overflow == sound.NoVoice {
		t.Fatal("the play that lost got no handle at all")
	}
	h.tick()

	got := h.probe(overflow)
	if got.Found {
		t.Fatal("the play that lost is in the view")
	}
	if got.Live != 1 || got.All[0].Voice != held {
		t.Fatalf("the view holds %d voices, want the one that kept its slot", got.Live)
	}
	if ended := h.waitEnded(); ended.Voice != overflow || ended.Reason != sound.ReasonStolen {
		t.Fatalf("ended as %v/%v, want %v/stolen", ended.Voice, ended.Reason, overflow)
	}
}

// The Device is polled once per flush and is a field read. Before the first
// tick there is nothing to have read, and afterwards it is whatever the Adapter
// says now.
func TestTheDeviceIsPolledEveryFlush(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 1, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	if got := h.probe(sound.NoVoice).Device; got.Ready || got.Name != "" {
		t.Fatalf("the Device reads %+v before the first flush", got)
	}

	h.tick()
	got := h.probe(sound.NoVoice).Device
	if !got.Ready || got.Name != "fake" || got.SampleRate != 48000 || got.Channels != 2 {
		t.Fatalf("the Device reads %+v after a flush", got)
	}
}

// countStarts is how many voice starts crossed the seam in total.
func countStarts(batches []sound.Batch) int {
	total := 0
	for _, batch := range batches {
		total += len(batch.Starts)
	}
	return total
}
