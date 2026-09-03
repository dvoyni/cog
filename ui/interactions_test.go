package ui

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

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

func TestHoverTrackerWaitsOutDwellOnAFallback(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	at := m.Vec2{X: 12, Y: 34}
	tracker.SetFallbackTooltip("pikeman", at)
	for frame := 0; frame < 4; frame++ {
		tracker.Update(hoverFrame(""), 0.1)
	}
	if text, _ := tracker.FallbackTooltip(); text != "" {
		t.Fatalf("FallbackTooltip = %q before the dwell elapsed; want \"\"", text)
	}
	tracker.Update(hoverFrame(""), 0.1)
	text, position := tracker.FallbackTooltip()
	if text != "pikeman" || position != at {
		t.Fatalf("FallbackTooltip = %q, %v; want %q, %v", text, position, "pikeman", at)
	}
}

// The fallback is the caller's own hit test, and what sits under a hand-drawn
// sprite is usually a screen-filling click catcher. Deferring to it would mean
// the fallback never showed at all.
func TestHoverTrackerFallbackOutranksAnElement(t *testing.T) {
	var tracker HoverTracker
	tracker.SetFallbackTooltip("pikeman", m.Vec2{})
	tracker.Update(hoverFrame("gameplay"), 0.1)
	if tracker.Hovered("gameplay") {
		t.Fatal("an element kept the hover while a fallback was set")
	}
	if text, _ := tracker.FallbackTooltip(); text != "pikeman" {
		t.Fatalf("FallbackTooltip = %q; want %q", text, "pikeman")
	}
}

func TestHoverTrackerClearsTheFallbackOnAnEmptyText(t *testing.T) {
	var tracker HoverTracker
	tracker.SetFallbackTooltip("pikeman", m.Vec2{})
	tracker.Update(hoverFrame("gameplay"), 0.1)
	tracker.SetFallbackTooltip("", m.Vec2{})
	tracker.Update(hoverFrame("gameplay"), 0.1)
	if text, _ := tracker.FallbackTooltip(); text != "" {
		t.Fatalf("FallbackTooltip = %q after clearing; want \"\"", text)
	}
	if !tracker.Hovered("gameplay") {
		t.Fatal("the element under the pointer did not get the hover back")
	}
}

// A tooltip already up follows the pointer, so the position is not gated by the
// dwell the way the text is.
func TestHoverTrackerFallbackFollowsThePointer(t *testing.T) {
	var tracker HoverTracker
	tracker.SetFallbackTooltip("pikeman", m.Vec2{X: 1, Y: 2})
	tracker.Update(hoverFrame(""), 0.1)
	moved := m.Vec2{X: 3, Y: 4}
	tracker.SetFallbackTooltip("pikeman", moved)
	if _, position := tracker.FallbackTooltip(); position != moved {
		t.Fatalf("FallbackTooltip position = %v; want %v", position, moved)
	}
}

// Replacing the text is moving to another target: with one tooltip already up,
// the next one shows at once rather than waiting out the dwell again.
func TestHoverTrackerSwitchesFallbacksInstantlyOnceShown(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	tracker.SetFallbackTooltip("pikeman", m.Vec2{})
	for frame := 0; frame < 5; frame++ {
		tracker.Update(hoverFrame(""), 0.1)
	}
	tracker.SetFallbackTooltip("voivode", m.Vec2{})
	tracker.Update(hoverFrame(""), 0.1)
	if text, _ := tracker.FallbackTooltip(); text != "voivode" {
		t.Fatalf("FallbackTooltip = %q; want %q", text, "voivode")
	}
}

// The element a fallback stands in for is usually a screen-filling click
// catcher, which the pointer is over the whole time it crosses the scene. That
// time must not spare a sprite its dwell.
func TestHoverTrackerFallbackDwellsAfterAnElement(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 10; frame++ {
		tracker.Update(hoverFrame("gameplay"), 0.1)
	}
	tracker.SetFallbackTooltip("pikeman", m.Vec2{})
	tracker.Update(hoverFrame("gameplay"), 0.1)
	if text, _ := tracker.FallbackTooltip(); text != "" {
		t.Fatalf("FallbackTooltip = %q on arrival; want it to wait out the dwell", text)
	}
	for frame := 0; frame < 4; frame++ {
		tracker.Update(hoverFrame("gameplay"), 0.1)
	}
	if text, _ := tracker.FallbackTooltip(); text != "pikeman" {
		t.Fatalf("FallbackTooltip = %q once the dwell elapsed; want %q", text, "pikeman")
	}
}

func TestHoverTrackerElementDwellsAfterAFallback(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	tracker.SetFallbackTooltip("pikeman", m.Vec2{})
	for frame := 0; frame < 5; frame++ {
		tracker.Update(hoverFrame("gameplay"), 0.1)
	}
	tracker.SetFallbackTooltip("", m.Vec2{})
	tracker.Update(hoverFrame("button"), 0.1)
	if tracker.Hovered("button") {
		t.Fatal("an element reached from a fallback should wait out the dwell again")
	}
}

// A sprite has no edges to hold the pointer inside, so a fallback's dwell runs
// only while the pointer holds still.
func TestHoverTrackerFallbackDwellNeedsAStillPointer(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 10; frame++ {
		tracker.SetFallbackTooltip("pikeman", m.Vec2{X: float32(frame)})
		tracker.Update(hoverFrame(""), 0.1)
	}
	if text, _ := tracker.FallbackTooltip(); text != "" {
		t.Fatalf("FallbackTooltip = %q while the pointer kept moving; want \"\"", text)
	}
	for frame := 0; frame < 5; frame++ {
		tracker.SetFallbackTooltip("pikeman", m.Vec2{X: 9})
		tracker.Update(hoverFrame(""), 0.1)
	}
	if text, _ := tracker.FallbackTooltip(); text != "pikeman" {
		t.Fatalf("FallbackTooltip = %q once the pointer held still; want %q", text, "pikeman")
	}
}

// Only a fallback's dwell waits on a still pointer. An element's is held by its
// own edges, and a caller that reports the pointer every frame while clearing
// the fallback must not stop every element tooltip.
func TestHoverTrackerElementDwellIgnoresPointerMovement(t *testing.T) {
	tracker := HoverTracker{Dwell: 0.4}
	for frame := 0; frame < 5; frame++ {
		tracker.SetFallbackTooltip("", m.Vec2{X: float32(frame)})
		tracker.Update(hoverFrame("button"), 0.1)
	}
	if !tracker.Hovered("button") {
		t.Fatal("an element's dwell should not wait on the pointer holding still")
	}
}
