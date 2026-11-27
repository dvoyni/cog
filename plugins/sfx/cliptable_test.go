package sfx_test

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// newFakeHarness answers a harness spawned with backend bound and plugins
// after it, bytes mounted at clipPath that only backend reads.
func newFakeHarness(t *testing.T, backend *fakeBackend, plugins ...kernel.Plugin) *sfxtest.Harness {
	t.Helper()
	h := sfxtest.NewHarness(t)
	h.UseBackend(backend)
	h.Spawn(nil, plugins...)
	h.Mount(clipPath, []byte("a clip the fake backend reads"))
	return h
}

// expectClipFailures has h expect every clip failure sfx logs, each named by
// its type: an ErrClipUnreadable, an ErrClipFailed or an ErrClipNotPrepared.
func expectClipFailures(h *sfxtest.Harness) {
	h.ExpectError(func(err error) bool {
		_, unreadable := errors.AsType[sfx.ErrClipUnreadable](err)
		_, failed := errors.AsType[sfx.ErrClipFailed](err)
		_, notPrepared := errors.AsType[sfx.ErrClipNotPrepared](err)
		return unreadable || failed || notPrepared
	})
}

// A play on a clip that is not resident makes its voice at once: silent,
// addressable, and starting from its offset with no catch-up when the clip
// installs. Catch-up would serve music sync and ruin one-shots, which are the
// overwhelming majority.
func TestAPlayBeforeItsClipIsReadyStartsFromItsOffsetWithNoCatchUp(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	const offset = 16 * sfxtest.StepInterval
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, offset, sfx.VoiceOptions{}) })
	for range 3 {
		h.Step()
	}
	waiting, live := readLiveVoice(h, voice)
	if !live {
		t.Fatal("a voice waiting on its clip does not exist")
	}
	if want := (sfx.VoiceEntry{Voice: voice, Path: clipPath, Playhead: offset, Audibility: 1}); waiting != want {
		t.Fatalf("a pending voice reads %+v, want %+v: its offset, no duration, and its bus folded in", waiting, want)
	}
	if starts := countStarts(seam.emitted()); starts != 0 {
		t.Fatalf("%d starts crossed the seam before the clip installed", starts)
	}

	backend.finishPrepares(nil)
	h.Step()

	playing, _ := readLiveVoice(h, voice)
	if want := offset + sfxtest.StepInterval; playing.Playhead != want {
		t.Fatalf("the voice is at %v, want %v: three flushes of waiting were caught up", playing.Playhead, want)
	}
	if playing.Duration != fakeClipSeconds*time.Second {
		t.Fatalf("the installed clip reads a duration of %v, want %v", playing.Duration, fakeClipSeconds*time.Second)
	}
	emitted := seam.emitted()
	if starts := countStarts(emitted); starts != 1 {
		t.Fatalf("%d starts crossed the seam, want one", starts)
	}
	if start := emitted[len(emitted)-1].Starts; len(start) != 1 || start[0].Offset != offset || start[0].Clip == 0 {
		t.Fatalf("the installing flush's starts are %+v, want one from %v on a real clip", start, offset)
	}
	wantEndings(t, h)
}

// A voice waiting on its clip is stoppable like any other: it ends with
// ReasonStopped at the flush that applies the stop, and since the backend
// never held it, nothing about it crosses the seam, not even once its clip
// installs.
func TestAPendingVoiceIsStoppedWithoutCrossingTheSeam(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := play(h)
	if _, live := readLiveVoice(h, voice); !live {
		t.Fatal("a voice waiting on its clip does not exist")
	}
	h.Record(func(queue sfx.Queue) { queue.Stop(voice) })
	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})

	backend.finishPrepares(nil)
	h.Step()

	if _, installCount := backend.counts(); installCount != 1 {
		t.Fatalf("the clip was installed %d times, want once", installCount)
	}
	for index, changes := range seam.emitted() {
		if len(changes.Starts) != 0 || len(changes.Stops) != 0 {
			t.Fatalf("flush %d carried %d starts and %d stops of a voice that never sounded", index+1, len(changes.Starts), len(changes.Stops))
		}
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})
}

// A clip that cannot be prepared is reported once, is terminal, and ends its
// voices with ReasonFailed, one or more flushes after the play, never in the
// same flush. A game cannot write "play it, and if it fails this frame, do
// X", and that is the rule rather than an accident of scheduling.
func TestAFailedClipEndsItsVoicesLaterAndNeverInTheSameFlush(t *testing.T) {
	refusal := errors.New("this clip is not ogg vorbis")
	backend := &fakeBackend{prepareErr: refusal}
	h := newFakeHarness(t, backend)
	expectClipFailures(h)

	voice := play(h)
	if _, live := readLiveVoice(h, voice); !live {
		t.Fatal("the voice was gone in the flush that applied its play")
	}
	wantEndings(t, h)

	h.Step()
	if _, live := readLiveVoice(h, voice); live {
		t.Fatal("the voice outlived the flush after its clip failed")
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed})

	// A second play of the clip finds its entry failed: no second read, no
	// second prepare and no second report, and its voice fails a flush later
	// too.
	again := play(h)
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed})
	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed}, sfx.VoiceEndedEvent{Voice: again, Reason: sfx.ReasonFailed})

	if prepareCount, _ := backend.counts(); prepareCount != 1 {
		t.Fatalf("the clip was prepared %d times, want once", prepareCount)
	}
	if failed := wantOneReport[sfx.ErrClipFailed](t, h); failed.Path != clipPath || !errors.Is(failed, refusal) {
		t.Fatalf("the report is %v, want one naming %q and carrying the backend's refusal", failed, clipPath)
	}
}

// A prepare that finishes with an error at a later flush fails the clip
// there, and its voices, which were waiting since an earlier flush, end in
// that same flush with ReasonFailed. Nothing is installed.
func TestAPrepareThatFailsLaterEndsItsVoicesAsFailed(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	h := newFakeHarness(t, backend)
	expectClipFailures(h)

	voice := play(h)
	h.Step()
	if _, live := readLiveVoice(h, voice); !live {
		t.Fatal("the voice was gone while its prepare was in flight")
	}

	refusal := errors.New("the stream is corrupt")
	backend.finishPrepares(refusal)
	h.Step()

	if _, live := readLiveVoice(h, voice); live {
		t.Fatal("the voice outlived the flush its clip failed in")
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed})
	if _, installCount := backend.counts(); installCount != 0 {
		t.Fatalf("%d IDs were minted for a clip whose prepare failed", installCount)
	}
	if failed := wantOneReport[sfx.ErrClipFailed](t, h); failed.Path != clipPath || !errors.Is(failed, refusal) {
		t.Fatalf("the report is %v, want one naming %q and carrying the refusal", failed, clipPath)
	}
}

// A backend that answers a prepare with neither a prepared clip nor an error,
// or installs one under the zero ID, has a bug: the clip fails for good with
// ErrClipNotPrepared, and its voices end with ReasonFailed.
func TestABackendThatPreparesNothingFailsTheClip(t *testing.T) {
	for _, test := range []struct {
		name    string
		backend *fakeBackend
	}{
		{"prepared nothing", &fakeBackend{preparingNothing: true}},
		{"installed under the zero ID", &fakeBackend{installingZero: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newFakeHarness(t, test.backend)
			expectClipFailures(h)

			voice := play(h)
			h.Step()

			wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed})
			if notPrepared := wantOneReport[sfx.ErrClipNotPrepared](t, h); notPrepared.Path != clipPath {
				t.Fatalf("the report is %v, want one naming %q", notPrepared, clipPath)
			}
		})
	}
}

// A path that is not there ends the voices recorded against it with
// ReasonFailed, a flush or more later, never in the flush that applied the
// play, and is reported once under its own path, so two missing clips are two
// reports. It is the one failure path a game can exercise on purpose, and the
// headless backend's real durations are what keep it reachable.
func TestAClipThatIsNotThereEndsItsVoicesAsFailed(t *testing.T) {
	h := newHarness(t, nil)
	expectClipFailures(h)

	const missingPath = "music/missing.ogg"
	const otherMissingPath = "music/also-missing.ogg"
	var voice, again, other sfx.Voice
	h.Record(func(queue sfx.Queue) {
		voice = queue.Play(missingPath, 0, sfx.VoiceOptions{})
		again = queue.Play(missingPath, 0, sfx.VoiceOptions{})
		other = queue.Play(otherMissingPath, 0, sfx.VoiceOptions{})
	})
	h.Step()
	if _, live := readLiveVoice(h, voice); !live {
		t.Fatal("the voice was gone in the flush that applied its play")
	}
	wantEndings(t, h)

	h.Step()
	if readLiveCount(h) != 0 {
		t.Fatalf("%d voices outlived the flush after their clips failed", readLiveCount(h))
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed},
		sfx.VoiceEndedEvent{Voice: again, Reason: sfx.ReasonFailed},
		sfx.VoiceEndedEvent{Voice: other, Reason: sfx.ReasonFailed})

	reports := h.Reports()
	if len(reports) != 2 {
		t.Fatalf("two missing clips were reported %d times, want once each: %v", len(reports), reports)
	}
	for index, path := range []string{missingPath, otherMissingPath} {
		if unreadable, isUnreadable := errors.AsType[sfx.ErrClipUnreadable](reports[index]); !isUnreadable || unreadable.Path != path || !errors.Is(reports[index], fs.ErrNotExist) {
			t.Errorf("report %d is %v, want an ErrClipUnreadable naming %q and carrying fs.ErrNotExist", index, reports[index], path)
		}
	}
}

// A clip the headless backend refuses, bytes that are not Ogg Vorbis, ends
// its voices with ReasonFailed a flush later, and is reported once, the
// backend's own error wrapped with the clip's path.
func TestAClipTheBackendRefusesEndsItsVoicesAsFailed(t *testing.T) {
	h := newHarness(t, nil)
	expectClipFailures(h)

	const refusedPath = "music/refused.ogg"
	h.Mount(refusedPath, []byte("not ogg vorbis"))
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(refusedPath, 0, sfx.VoiceOptions{}) })
	h.Step()
	wantEndings(t, h)
	h.Step()

	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFailed})
	failed := wantOneReport[sfx.ErrClipFailed](t, h)
	if _, isNotOggVorbis := errors.AsType[headless.ErrNotOggVorbis](failed); failed.Path != refusedPath || !isNotOggVorbis {
		t.Fatalf("the report is %v, want one naming %q and carrying the headless backend's ErrNotOggVorbis", failed, refusedPath)
	}
}

// wantOneReport answers the one error h has logged, failing the test unless
// there is exactly one and it is an E.
func wantOneReport[E error](t *testing.T, h *sfxtest.Harness) E {
	t.Helper()
	reports := h.Reports()
	if len(reports) != 1 {
		t.Fatalf("%d errors were logged, want one: %v", len(reports), reports)
	}
	report, isReport := errors.AsType[E](reports[0])
	if !isReport {
		t.Fatalf("the error logged is %v, want a %T", reports[0], report)
	}
	return report
}

// countStarts answers how many starts crossed the seam in changes.
func countStarts(changes []sfx.FlushChanges) int {
	count := 0
	for _, flushed := range changes {
		count += len(flushed.Starts)
	}
	return count
}
