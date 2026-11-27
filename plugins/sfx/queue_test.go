package sfx_test

import (
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
)

// The handle is the index, and it is minted when the play is recorded, so a
// game reads a real voice back from Play and can address it before the flush
// that applies it.
func TestAHandleIsMintedWhenThePlayIsRecorded(t *testing.T) {
	h := newHarness(t, nil)

	var first, second sfx.Voice
	h.Record(func(queue sfx.Queue) {
		first = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		second = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		if first == sfx.NoVoice || second == sfx.NoVoice {
			t.Errorf("Play handed back %v and %v; a play always gets a real handle", first, second)
		}
	})
	h.Step()

	if first == second {
		t.Fatal("two plays in one flush got one handle")
	}
	for _, voice := range []sfx.Voice{first, second} {
		if _, live := readLiveVoice(h, voice); !live {
			t.Fatalf("%v, minted at its play, addresses nothing after the flush", voice)
		}
	}
}

// Generations start at 1, so Voice(0) means no voice while index 0 stays an
// ordinary slot, and a reused slot hands out a handle the one before it does
// not compare equal to.
func TestASlotIsHandedOutAgainAtTheNextGeneration(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 1})

	first := play(h)
	if got := first.String(); got != "Voice(0v1)" {
		t.Fatalf("the first handle renders %s, want Voice(0v1)", got)
	}

	// The slots come back at the end of the flush that ended their voices,
	// so the stop's flush frees slot 0 for the next play.
	h.Record(func(queue sfx.Queue) { queue.Stop(first) })
	h.Step()
	second := play(h)
	if got := second.String(); got != "Voice(0v2)" {
		t.Fatalf("the reused slot renders %s, want Voice(0v2)", got)
	}
	if _, live := readLiveVoice(h, first); live {
		t.Fatal("the handle before the reuse addresses the voice that took its slot")
	}
}

// A play at a full table still gets a real handle. It names no slot, which is
// what makes it addressable and stale at once: the voice it named is gone by
// the time anybody could ask.
func TestAPlayThatFindsNoSlotStillGetsARealHandle(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 1})

	var held, overflow sfx.Voice
	h.Record(func(queue sfx.Queue) {
		held = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		overflow = queue.Play(clipPath, 0, sfx.VoiceOptions{})
	})
	h.Step()

	if overflow == sfx.NoVoice || overflow == held {
		t.Fatalf("the overflowing play got %v, want a handle of its own", overflow)
	}
	if _, live := readLiveVoice(h, overflow); live {
		t.Fatal("a handle that names no slot addressed a voice")
	}
}
