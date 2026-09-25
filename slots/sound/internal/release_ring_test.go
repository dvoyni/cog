package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The ring names the Clip of every ending, and a release is the one ending
// whose Clip nothing else in the Slot could have supplied: issue 487 put Clip
// on Ending and filled in every literal it could see, and issue 484 added the
// two release endings a commit later, where a zero ClipRef compiles and renders
// as an empty name.
//
// An agent asking why the ambience stopped is exactly the reader that loses by
// it - it would be told that something was released without being told what -
// and neither ticket could have caught it alone, because 484 knew nothing of
// the ring and 487 could not make ReasonReleased fire.
func TestTheRingNamesTheClipOfAReleasedVoice(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, twoClips)

	h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	h.play(ClipWithResource(drum), 0, Params{Loop: m.Some(true)})
	h.tick()

	h.record(func(queue *Queue) { queue.Release(ClipWithResource(bell)) })
	h.tick()

	got := listing(t, h)
	var released []endingView
	for _, ending := range got.Endings {
		if ending.Reason == ReasonReleased.String() {
			released = append(released, ending)
		}
	}
	if len(released) != 1 {
		t.Fatalf("the ring holds %d released endings, want the one Voice on the released Clip", len(released))
	}
	if released[0].Clip != bell {
		t.Fatalf("a released Voice is in the ring as %q, want %q", released[0].Clip, bell)
	}
}
