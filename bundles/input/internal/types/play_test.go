package types

import (
	"testing"
	"time"
)

// One rule produces every idiom: a delay splits the sequence into another
// dispatch, and nothing else does.
func TestPlan_ADelayIsTheOnlyThingThatSplitsASequence(t *testing.T) {
	click := []Action{
		{Do: ActionMove, X: 10, Y: 20},
		{Do: ActionKeyDown, Key: KeyMouseLeft},
		{Do: ActionKeyUp, Key: KeyMouseLeft},
	}
	if batches := plan(click); len(batches) != 1 || len(batches[0].actions) != 3 {
		t.Errorf("a click planned as %d batches, want one of three steps", len(batches))
	}

	hold := []Action{
		{Do: ActionKeyDown, Key: KeyW},
		{Do: ActionDelay, Ms: 500},
		{Do: ActionKeyUp, Key: KeyW},
	}
	batches := plan(hold)
	if len(batches) != 2 {
		t.Fatalf("a held press planned as %d batches, want 2", len(batches))
	}
	if batches[0].delay != 0 {
		t.Errorf("the first batch waits %s, want nothing", batches[0].delay)
	}
	if batches[1].delay != 500*time.Millisecond {
		t.Errorf("the wait before the second batch is %s, want 500ms", batches[1].delay)
	}

	drag := []Action{
		{Do: ActionMove, X: 1, Y: 1},
		{Do: ActionKeyDown, Key: KeyMouseLeft},
		{Do: ActionDelay, Ms: 100},
		{Do: ActionMove, X: 9, Y: 9},
		{Do: ActionKeyUp, Key: KeyMouseLeft},
	}
	if batches := plan(drag); len(batches) != 2 ||
		len(batches[0].actions) != 2 || len(batches[1].actions) != 2 {
		t.Errorf("a drag planned as %+v, want two batches of two steps", batches)
	}

	if batches := plan(nil); len(batches) != 1 || len(batches[0].actions) != 0 {
		t.Errorf("an empty sequence planned as %d batches, want one empty one", len(batches))
	}
	if batches := plan([]Action{{Do: ActionDelay, Ms: 5}}); len(batches) != 2 {
		t.Errorf("a trailing delay planned as %d batches, want its wait honoured", len(batches))
	}
}
