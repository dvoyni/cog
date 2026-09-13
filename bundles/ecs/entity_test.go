package ecs

import (
	"fmt"
	"math"
	"testing"
)

// Entity's index/generation split is deliberately private (cog#237), so what a
// test may hold it to is the behaviour the split exists to give, never the
// layout: the zero value is NoEntity, index 0 is an ordinary slot, and the two
// halves survive a round trip at their extremes.

func TestZeroEntityIsNoEntityAndIndexZeroStaysOrdinary(t *testing.T) {
	if NoEntity != 0 {
		t.Fatalf("NoEntity = %d, want the zero value", uint64(NoEntity))
	}
	first := newEntity(0, 1)
	if first == NoEntity {
		t.Fatalf("the first entity at index 0 = %v, want something other than NoEntity", first)
	}
	if first.idx() != 0 {
		t.Fatalf("index = %d, want 0: index 0 is a usable slot, not a sentinel", first.idx())
	}
}

func TestEntityRoundTripsBothHalves(t *testing.T) {
	cases := []struct{ index, generation uint32 }{
		{0, 1},
		{1, 1},
		{7, 2},
		{math.MaxUint32, 1},
		{0, math.MaxUint32 - 1},
		{math.MaxUint32, math.MaxUint32 - 1},
	}
	for _, test := range cases {
		e := newEntity(test.index, test.generation)
		if e.idx() != test.index || e.gen() != test.generation {
			t.Fatalf("newEntity(%d, %d) reads back as (%d, %d)",
				test.index, test.generation, e.idx(), e.gen())
		}
	}
}

func TestEntityStrings(t *testing.T) {
	var _ fmt.Stringer = NoEntity
	if got := NoEntity.String(); got != "NoEntity" {
		t.Fatalf("NoEntity.String() = %q, want %q", got, "NoEntity")
	}
	if got := newEntity(7, 2).String(); got != "Entity(7v2)" {
		t.Fatalf("String() = %q, want %q", got, "Entity(7v2)")
	}
}

func TestEntityIsAMapKey(t *testing.T) {
	seen := map[Entity]int{}
	seen[newEntity(3, 1)]++
	seen[newEntity(3, 1)]++
	seen[newEntity(3, 2)]++
	if seen[newEntity(3, 1)] != 2 {
		t.Fatalf("one entity counted %d times, want 2", seen[newEntity(3, 1)])
	}
	if len(seen) != 2 {
		t.Fatalf("%d distinct keys, want 2: two generations of one index are two entities", len(seen))
	}
}
