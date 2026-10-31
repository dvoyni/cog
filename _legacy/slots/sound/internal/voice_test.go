package internal

import "testing"

// The handle is the index, and it is minted when the play is recorded - so a
// game reads a real Voice back from Play and can address it before the tick
// that recorded it has flushed.
func TestAHandleIsMintedWhenThePlayIsRecorded(t *testing.T) {
	queue := NewQueue(4)

	first := queue.Play(ClipWithResource("a.ogg"), 0, Params{})
	second := queue.Play(ClipWithResource("b.ogg"), 0, Params{})

	if first == NoVoice || second == NoVoice {
		t.Fatalf("Play handed back %v and %v; a play always gets a real handle", first, second)
	}
	if first == second {
		t.Fatal("two plays in one tick got one handle")
	}
	if got := len(QueueOperations(queue)); got != 2 {
		t.Fatalf("the queue recorded %d operations, want 2", got)
	}
}

// Generations start at 1, so Voice(0) unambiguously means no Voice while index
// 0 stays an ordinary usable slot - and a recycled slot hands out a handle the
// one before it does not compare equal to.
func TestASlotIsHandedOutAgainAtTheNextGeneration(t *testing.T) {
	queue := NewQueue(1)
	voices, buses, listener := NewVoices(1), NewBuses(), NewListener()

	first := queue.Play(ClipWithResource("a.ogg"), 0, Params{})
	if got := first.String(); got != "Voice(0v1)" {
		t.Fatalf("the first handle renders %s, want Voice(0v1)", got)
	}

	// The slots come back from the table rather than one ending at a time, so
	// a flush that leaves the table empty is what frees slot 0 again.
	QueueRank(queue, voices, buses, listener)
	second := queue.Play(ClipWithResource("a.ogg"), 0, Params{})
	if got := second.String(); got != "Voice(0v2)" {
		t.Fatalf("the recycled slot renders %s, want Voice(0v2)", got)
	}
	if first == second {
		t.Fatal("a recycled slot handed out the handle it had before")
	}
}

// A play at a full table still gets a real handle. It names no slot, which is
// what makes it addressable and stale at once: the Voice it named is already
// gone by the time anybody could ask.
func TestAPlayThatFindsNoSlotStillGetsARealHandle(t *testing.T) {
	queue := NewQueue(1)
	voices := NewVoices(1)

	held := queue.Play(ClipWithResource("a.ogg"), 0, Params{})
	overflow := queue.Play(ClipWithResource("b.ogg"), 0, Params{})

	if overflow == NoVoice || overflow == held {
		t.Fatalf("the overflowing play got %v, want a handle of its own", overflow)
	}
	if _, ok := voices.Info(overflow); ok {
		t.Fatal("a handle that names no slot addressed a Voice")
	}
}

// The zero handle renders as what it means, and every other one shows both
// halves: a log that cannot tell a recycled index from the handle before it is
// useless.
func TestVoiceRendersBothHalves(t *testing.T) {
	if got := NoVoice.String(); got != "NoVoice" {
		t.Fatalf("NoVoice renders %s", got)
	}
	if got := newVoice(7, 2).String(); got != "Voice(7v2)" {
		t.Fatalf("index 7 at generation 2 renders %s, want Voice(7v2)", got)
	}
}
