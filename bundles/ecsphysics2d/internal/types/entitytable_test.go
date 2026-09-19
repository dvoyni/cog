package types

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
)

// TestTheEntityTableFindsEveryRowItWasGiven is the map that answers for the
// jointed Bodies detection never saw, and the one place a Joint pays a hash.
func TestTheEntityTableFindsEveryRowItWasGiven(t *testing.T) {
	var table entityTable
	table.reset()
	if _, ok := table.lookup(partyA); ok {
		t.Fatal("a fresh table found a row")
	}

	const count = 300
	for i := range count {
		table.put(ecs.Entity(i+1), int32(i+1))
	}
	for i := range count {
		at, ok := table.lookup(ecs.Entity(i + 1))
		if !ok || at != int32(i+1) {
			t.Fatalf("%d read back as %d, %v", i+1, at, ok)
		}
	}
	if _, ok := table.lookup(ecs.Entity(count + 1)); ok {
		t.Error("the table found a row nothing put there")
	}

	// Reset keeps the memory and forgets the rows, which is what makes the
	// per-tick rebuild allocate nothing.
	table.reset()
	if table.used != 0 {
		t.Errorf("the reset table still holds %d rows", table.used)
	}
	if _, ok := table.lookup(ecs.Entity(1)); ok {
		t.Error("the reset table still finds a row")
	}
}
