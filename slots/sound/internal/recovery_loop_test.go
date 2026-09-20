package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// A recovery restarts every live Voice, and a restart is the only sentence the
// seam has for "this Voice is looping" - VoiceUpdate carries no Loop. So a
// resync that forgets the flag silently un-loops the music the moment the
// headphones go back in, and nothing else in the Slot would ever notice: the
// view still says Loop, sound's own playhead still wraps, and only the Adapter
// disagrees.
//
// This is the seam between issue 481, which set Loop on the ordinary start, and
// issue 485, which added the resync arm a commit earlier and could not have
// carried a field that did not exist yet.
func TestARecoveryKeepsALoopingVoiceLooping(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.play(sound.ClipWithResource(bell), 0, sound.Params{Loop: m.Some(true)})
	h.tick()

	first := h.backend.emitted()
	if got := first[len(first)-1].Starts; len(got) != 1 || !got[0].Loop {
		t.Fatalf("the ordinary start crossed with Loop %v, want true", got[0].Loop)
	}

	backend.setReady(false)
	for range 10 {
		h.tick()
	}
	backend.setReady(true)
	h.tick()

	batches := h.backend.emitted()
	last := batches[len(batches)-1]
	if len(last.Starts) != 1 {
		t.Fatalf("recovery started %d Voices, want the one that is live", len(last.Starts))
	}
	if !last.Starts[0].Loop {
		t.Fatal("a looping Voice came back from a lost Device as a one-shot")
	}
}
