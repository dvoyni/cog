package ui

import "testing"

func hoverFrame(id ID) *Interactions {
	if id == "" {
		return &Interactions{}
	}
	return &Interactions{values: []Interaction{{ID: id, Kind: InteractionHover, Button: -1}}}
}

func TestHoverTrackerReportsImmediatelyWithoutDwell(t *testing.T) {
	var tracker HoverTracker
	tracker.Update(hoverFrame("button"), 0.1)
	if !tracker.Hovered("button") {
		t.Fatal("zero dwell should report on the first frame")
	}
	if tracker.Hovered("other") {
		t.Fatal("reported an element the pointer is not over")
	}
}

func TestHoverTrackerWaitsOutDwell(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 4; frame++ {
		tracker.Update(hoverFrame("button"), 0.1)
	}
	if tracker.Hovered("button") {
		t.Fatal("reported before the dwell elapsed")
	}
	tracker.Update(hoverFrame("button"), 0.1)
	if !tracker.Hovered("button") {
		t.Fatal("did not report once the dwell elapsed")
	}
}

func TestHoverTrackerSwitchesInstantlyOnceShown(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 5; frame++ {
		tracker.Update(hoverFrame("first"), 0.1)
	}
	tracker.Update(hoverFrame("second"), 0.1)
	if !tracker.Hovered("second") {
		t.Fatal("moving straight to a neighbour should not wait again")
	}
	if tracker.Hovered("first") {
		t.Fatal("still reported the element the pointer left")
	}
}

func TestHoverTrackerRestartsDwellAfterLeavingEverything(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 5; frame++ {
		tracker.Update(hoverFrame("first"), 0.1)
	}
	tracker.Update(hoverFrame(""), 0.1)
	if tracker.Hovered("first") {
		t.Fatal("still reported after the pointer left every element")
	}
	tracker.Update(hoverFrame("second"), 0.1)
	if tracker.Hovered("second") {
		t.Fatal("the next element should wait out the dwell again")
	}
}

// Sweeping across a toolbar without resting on anything must not accumulate
// into a tooltip on whichever element the pointer happens to reach.
func TestHoverTrackerDoesNotAccumulateAcrossASweep(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for _, id := range []ID{"a", "b", "c", "d", "e"} {
		tracker.Update(hoverFrame(id), 0.1)
	}
	if tracker.Hovered("e") {
		t.Fatal("a sweep should not add up to a dwell")
	}
}
