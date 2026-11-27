package sfx_test

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// A seek is block-accurate and never sample-accurate: in a mixer it is a
// cursor move, and in Web Audio a fresh start(when, offset), so promising
// sample-exactness would foreclose that backend. What crosses the seam is
// exactly that, a VoiceStart carrying an Offset, and no update beside it.
func TestASeekMovesThePlayheadAndCrossesTheSeamAsAStart(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 500*time.Millisecond) })
	h.Step()

	// The seek lands the playhead and the same flush advances it by a step,
	// as a play's does.
	if got, want := readPlayhead(h, voice), 500*time.Millisecond+sfxtest.StepInterval; got != want {
		t.Fatalf("the seeked playhead is %v, want %v", got, want)
	}
	changes := seam.emitted()[1]
	if len(changes.Starts) != 1 || len(changes.Updates) != 0 {
		t.Fatalf("the seeking flush emitted %+v, want the one start a seek is", changes)
	}
	if got := changes.Starts[0].Offset; got != 500*time.Millisecond {
		t.Fatalf("the start carries an offset of %v, want 500ms", got)
	}
}

// A negative offset clamps to zero rather than being refused: no operation
// returns an error, and there is nowhere to put one. Past the end is the clip
// reaching its end, which is what it is called when a playhead gets there by
// itself.
func TestASeekClampsBelowZeroAndEndsAOneShotPastTheEnd(t *testing.T) {
	h := newHarness(t, nil)

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.Seek(voice, -3*time.Second) })
	h.Step()
	if got := readPlayhead(h, voice); got != sfxtest.StepInterval {
		t.Fatalf("a seek to -3s left the playhead at %v, want a clamp to zero and one step", got)
	}

	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 9*time.Second) })
	h.Step()
	if _, live := readLiveVoice(h, voice); live {
		t.Fatalf("%v survived a seek past the end of its clip", voice)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
}

// A seek on a voice still waiting on its clip moves where it will start: it
// has no playhead the backend holds and no start to restate, and it was going
// to begin from its offset with no catch-up anyway.
func TestASeekOnAPendingVoiceMovesWhereItWillStart(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 2*time.Second) })
	h.Step()
	if got := readPlayhead(h, voice); got != 2*time.Second {
		t.Fatalf("the pending voice reads a playhead of %v after its seek, want 2s", got)
	}
	if starts := countStarts(seam.emitted()); starts != 0 {
		t.Fatalf("a seek on a pending voice crossed the seam as %d starts", starts)
	}

	backend.finishPrepares(nil)
	h.Step()
	if starts := lastChanges(seam).Starts; len(starts) != 1 || starts[0].Offset != 2*time.Second {
		t.Fatalf("the installing flush's starts are %+v, want one from the seeked 2s", starts)
	}
	if got := readPlayhead(h, voice); got != 2*time.Second+sfxtest.StepInterval {
		t.Fatalf("the voice is at %v once its clip installed, want %v", got, 2*time.Second+sfxtest.StepInterval)
	}
}

// readPlayhead answers voice's playhead in the view, zero when it is gone.
func readPlayhead(h *sfxtest.Harness, voice sfx.Voice) time.Duration {
	entry, _ := readLiveVoice(h, voice)
	return entry.Playhead
}
