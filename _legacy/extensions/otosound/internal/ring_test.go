//go:build !js

package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/slots/sound"
)

// The handoff's tests run with -count=10 rather than under the race detector,
// which cannot build in this environment. They are written so that a broken
// protocol shows as a lost or reordered operation rather than as a data race,
// which is what makes repetition worth anything at all here.

// drained applies the ring the way the Mixer applies it - every published batch
// in order, then one store of the applied counter - and reports the operations
// that arrived, so a test can assert the order and not only the outcome.
func drained(handoff *ring) []op {
	var got []op
	write := handoff.write.Load()
	read := handoff.read.Load()
	for ; read < write; read++ {
		got = append(got, handoff.slots[read%ringDepth].ops...)
	}
	handoff.read.Store(read)
	return got
}

// tick is one publish: record a start naming clip id, then hand it over,
// merging into the staging batch when the ring is full.
func tick(handoff *ring, id sound.ClipID) {
	handoff.record(op{kind: opStart, slot: sound.VoiceSlot(int(id) % 8), id: id}, handoff.merging())
	handoff.publish()
}

func ids(ops []op) []sound.ClipID {
	got := make([]sound.ClipID, len(ops))
	for i := range ops {
		got[i] = ops[i].id
	}
	return got
}

func wantIdsInOrder(t *testing.T, got []sound.ClipID, total int) {
	t.Helper()
	if len(got) != total {
		t.Fatalf("%d operations arrived, want %d - the handoff lost some", len(got), total)
	}
	for i, id := range got {
		if want := sound.ClipID(i + 1); id != want {
			t.Fatalf("operation %d is clip %d, want %d - the handoff reordered them", i, id, want)
		}
	}
}

// Several batches can land in one block, and they are applied in order rather
// than coalesced: two ticks' operations taking effect at one instant is
// correct, because they were ordered and they stay ordered.
func TestEveryPublishedBatchIsAppliedWholeAndInOrder(t *testing.T) {
	handoff := newRing(8)
	for i := 1; i <= 3; i++ {
		tick(handoff, sound.ClipID(i))
	}
	if handoff.pending() != 3 {
		t.Fatalf("%d batches are pending, want 3", handoff.pending())
	}

	wantIdsInOrder(t, ids(drained(handoff)), 3)

	if handoff.pending() != 0 {
		t.Fatalf("%d batches are still pending after a drain", handoff.pending())
	}
}

// A full ring means the device thread has not run for eight ticks. It neither
// blocks the game nor drops the starts the contract promised to keep: the tick
// keeps accumulating in a staging batch the Mixer cannot be reading, and
// publishes it when there is room.
func TestAFullRingMergesIntoStagingRatherThanBlockingOrDropping(t *testing.T) {
	const total = ringDepth * 3
	handoff := newRing(64)

	for i := 1; i <= total; i++ {
		tick(handoff, sound.ClipID(i))
	}
	if handoff.pending() != ringDepth {
		t.Fatalf("%d batches are pending, want the ring's %d", handoff.pending(), ringDepth)
	}
	if !handoff.merging() {
		t.Fatal("the staging batch is empty, so the operations a full ring could not take were dropped")
	}

	got := drained(handoff)
	handoff.publish()
	got = append(got, drained(handoff)...)

	wantIdsInOrder(t, ids(got), total)
	if handoff.merging() {
		t.Fatal("the staging batch still holds operations after there was room to publish them")
	}
}

// A stop in one tick and a start of the same slot in the next are two
// operations whose order decides whether the second Voice sounds at all. This
// is the failing sequence four kind-by-kind slices cannot express once two
// ticks have been merged, and it is why a batch here is an ordered list.
func TestAStartAfterAStopOnOneSlotSurvivesTheMerge(t *testing.T) {
	handoff := newRing(8)
	mx := newMixer(handoff, 8)
	clip := constantClip(1024, 1, 1)

	// Fill the ring so that everything after it merges.
	for i := 1; i <= ringDepth; i++ {
		tick(handoff, sound.ClipID(i))
	}
	handoff.record(op{kind: opStop, slot: 0}, true)
	handoff.record(op{
		kind:   opStart,
		slot:   0,
		clip:   clip,
		params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, true)
	handoff.publish()

	mx.drain()
	handoff.publish()
	mx.drain()

	if !mx.voices[0].active {
		t.Fatal("the start was killed by a stop recorded before it")
	}
	if mx.voices[0].clip != clip {
		t.Fatal("the slot is not reading the Clip the surviving start named")
	}
}

// An update coalesces into the last operation standing for its slot, which is
// the same coalescing sound performs within a tick widened over the ticks a
// merge covers. A start and a stop never coalesce: they are what the contract
// promises not to lose.
func TestAMergeCoalescesUpdatesAndKeepsStartsAndStops(t *testing.T) {
	handoff := newRing(8)
	handoff.record(op{kind: opStart, slot: 1, params: sound.VoiceParams{Gains: mono(0.1), Rate: 1}}, true)
	handoff.record(op{kind: opUpdate, slot: 1, params: sound.VoiceParams{Gains: mono(0.5), Rate: 1}}, true)
	handoff.record(op{kind: opUpdate, slot: 1, params: sound.VoiceParams{Gains: mono(0.9), Rate: 1}}, true)
	handoff.record(op{kind: opStop, slot: 2}, true)
	handoff.record(op{kind: opStop, slot: 3}, true)

	ops := handoff.staging.ops
	if len(ops) != 3 {
		t.Fatalf("the merge kept %d operations, want a start, and two stops, with the updates folded in", len(ops))
	}
	if ops[0].kind != opStart || ops[0].params.Gains != mono(0.9) {
		t.Fatalf("the updates did not fold into the start: %+v", ops[0].params.Gains)
	}
	if ops[1].kind != opStop || ops[2].kind != opStop {
		t.Fatal("a stop was coalesced away, and a stop is never coalesced")
	}
}

// The applied counter is the tick's permission to free, so it must not move
// before the batch it names has been applied whole.
func TestTheAppliedCounterOnlyMovesOnceABatchHasBeenApplied(t *testing.T) {
	handoff := newRing(8)
	mx := newMixer(handoff, 8)
	tick(handoff, 1)

	if handoff.applied() != 0 {
		t.Fatalf("the applied counter is %d before any block", handoff.applied())
	}
	mx.drain()
	if handoff.applied() != 1 {
		t.Fatalf("the applied counter is %d after one batch was applied, want 1", handoff.applied())
	}
}

// The protocol, exercised from two goroutines at once. There is no race
// detector in this environment, so what this can check is the property rather
// than the memory model: with a producer publishing as fast as it can and a
// consumer draining as fast as it can, every operation arrives exactly once and
// in the order it was recorded. Run it with -count=10.
func TestNoOperationIsLostWhenTheTickAndTheMixerRunTogether(t *testing.T) {
	const total = 4000
	handoff := newRing(64)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 1; i <= total; i++ {
			tick(handoff, sound.ClipID(i))
		}
		for handoff.merging() {
			if _, published := handoff.publish(); !published {
				time.Sleep(time.Microsecond)
			}
		}
	}()

	var got []sound.ClipID
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < total {
		got = append(got, ids(drained(handoff))...)
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d operations arrived before the deadline", len(got), total)
		}
	}
	<-done
	got = append(got, ids(drained(handoff))...)

	wantIdsInOrder(t, got, total)
}
