package types

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
)

// noJoints is the empty JointedPairs every Contact test hands Collide: those
// tests are about detection, and a scene with no Joint has no pair held apart.
// It is one value rather than a fresh set per call, so a benchmark's loop still
// allocates nothing.
var noJoints = NewJointedPairs()

// TestJointedPairsHoldsAnUnorderedPairAndTakesADuplicateOnce is the set Index
// builds and Detect checks. It is unordered because which party is A is
// detection's choice, and it takes a duplicate because two Joints between the
// same two Bodies are an ordinary thing to spawn.
func TestJointedPairsHoldsAnUnorderedPairAndTakesADuplicateOnce(t *testing.T) {
	pairs := NewJointedPairs()
	if pairs.Len() != 0 || pairs.Has(partyA, partyB) {
		t.Fatal("a fresh set is not empty")
	}

	pairs.Add(partyA, partyB)
	if pairs.Len() != 1 {
		t.Errorf("one pair made the set %d long", pairs.Len())
	}
	if !pairs.Has(partyA, partyB) || !pairs.Has(partyB, partyA) {
		t.Error("the pair is not found both ways round")
	}

	pairs.Add(partyB, partyA)
	if pairs.Len() != 1 {
		t.Errorf("the same pair the other way round made the set %d long", pairs.Len())
	}

	if pairs.Has(partyA, ecs.Entity(99)) {
		t.Error("a pair nothing added is in the set")
	}

	pairs.Clear()
	if pairs.Len() != 0 || pairs.Has(partyA, partyB) {
		t.Error("the cleared set still holds a pair")
	}
}

// TestJointedPairsGrowsWithoutLosingAPair is the doubling the table does when
// it is half full, which a scene whose Joint count climbs walks straight into.
func TestJointedPairsGrowsWithoutLosingAPair(t *testing.T) {
	pairs := NewJointedPairs()
	const count = 500
	for i := range count {
		pairs.Add(ecs.Entity(2*i+1), ecs.Entity(2*i+2))
	}
	if pairs.Len() != count {
		t.Fatalf("the set holds %d pairs, want %d", pairs.Len(), count)
	}
	for i := range count {
		if !pairs.Has(ecs.Entity(2*i+2), ecs.Entity(2*i+1)) {
			t.Fatalf("the pair %d was lost across a growth", i)
		}
	}
}

// TestTheJointedPairSetAllocatesNothingOnceItHasGrown is the zero-allocation
// rule on the one structure Index rebuilds every tick.
func TestTheJointedPairSetAllocatesNothingOnceItHasGrown(t *testing.T) {
	pairs := NewJointedPairs()
	fill := func() {
		pairs.Clear()
		for i := range 256 {
			pairs.Add(ecs.Entity(2*i+1), ecs.Entity(2*i+2))
		}
	}
	fill()
	if got := testing.AllocsPerRun(50, fill); got != 0 {
		t.Errorf("rebuilding the set allocates %v objects a tick, want none", got)
	}
}
