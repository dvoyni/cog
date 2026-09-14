package types

import "testing"

func TestChildrenDoesNotCopyTheFirstSequence(t *testing.T) {
	children := []Element{NewElement(), NewElement(), NewElement()}
	var sink Element
	allocations := testing.AllocsPerRun(100, func() {
		sink = NewElement().Children(children...)
	})
	_ = sink
	if allocations != 0 {
		t.Fatalf("first Children allocations = %v, want 0", allocations)
	}
}

// A second Children call must append into fresh storage rather than the borrowed
// array, so independent branches off one element cannot overwrite each other or
// the callers spare capacity.
func TestChildrenBranchesDoNotShareBorrowedStorage(t *testing.T) {
	shared := make([]Element, 2, 8)
	shared[0] = NewElement().ID("a")
	shared[1] = NewElement().ID("b")

	base := NewElement().Children(shared...)
	first := base.Children(NewElement().ID("first"))
	second := base.Children(NewElement().ID("second"))

	if got := first.children[2].id; got != "first" {
		t.Errorf("first branch third child = %q, want first", got)
	}
	if got := second.children[2].id; got != "second" {
		t.Errorf("second branch third child = %q, want second", got)
	}
	for index, child := range shared[:cap(shared)][2:] {
		if child.id != "" {
			t.Errorf("caller spare capacity slot %d written: %q", index, child.id)
		}
	}
}

var interactionSum int

func TestInteractionsIterationDoesNotAllocate(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "first", Kind: InteractionHover, Button: -1},
		{ID: "second", Kind: InteractionClick, Button: 0},
	}}
	if found, _ := interactions.Has("second", InteractionClick, 0, false); !found {
		t.Fatal("Has did not find interaction")
	}

	allocations := testing.AllocsPerRun(100, func() {
		total := 0
		for interaction := range interactions.All() {
			total += interaction.Button
		}
		interactionSum = total
	})
	if allocations != 0 {
		t.Fatalf("All allocations = %v, want 0", allocations)
	}
}

func TestInteractionsHasUsesAndConsumesTopmostKind(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "bottom", Kind: InteractionClick, Button: 0},
		{ID: "hovered", Kind: InteractionHover, Button: -1},
		{ID: "top", Kind: InteractionClick, Button: 0},
	}}

	if found, _ := interactions.Has("bottom", InteractionClick, 0, true); found {
		t.Fatal("Has found a click below the topmost click")
	}
	if len(interactions.values) != 3 {
		t.Fatalf("failed Has consumed interactions: got %d values, want 3", len(interactions.values))
	}
	if found, _ := interactions.Has("top", InteractionClick, 0, true); !found {
		t.Fatal("Has did not find the topmost click")
	}
	if len(interactions.values) != 1 || interactions.values[0].Kind != InteractionHover {
		t.Fatalf("consumed interactions = %v, want only hover", interactions.values)
	}
}

func TestInteractionsHasReturnsTopmostUserData(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "bottom", Kind: InteractionClick, Button: 0},
		{ID: "top", Kind: InteractionClick, Button: 0, userData: "top data"},
	}}

	found, userData := interactions.Has("top", InteractionClick, 0, false)
	if !found || userData != "top data" {
		t.Fatalf("Has result = %v, %q; want true, %q", found, userData, "top data")
	}
}

func TestInteractionsClearLeavesNothingToFindAndKeepsTheBuffer(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "clicked", Kind: InteractionClick, Button: 0},
		{ID: "hovered", Kind: InteractionHover, Button: -1},
	}}
	buffer := interactions.values

	interactions.Clear()

	if len(interactions.values) != 0 {
		t.Fatalf("cleared interactions = %v, want none", interactions.values)
	}
	if interactions.Clicked("clicked") {
		t.Fatal("Clicked found a click after Clear")
	}
	for range interactions.All() {
		t.Fatal("All yielded an interaction after Clear")
	}
	if cap(interactions.values) != cap(buffer) {
		t.Fatalf("Clear replaced the buffer: cap = %d, want %d", cap(interactions.values), cap(buffer))
	}
}
