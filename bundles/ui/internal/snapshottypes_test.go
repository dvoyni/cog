package internal

import "testing"

// A built-in visual is declared in internal but is ui's, so a layout snapshot
// names it after ui - what the application imports, and what ui_layout
// reported before the declarations moved here. A type declared anywhere else
// keeps its own package's name.
func TestABuiltInVisualIsNamedAfterUI(t *testing.T) {
	visual, params := Sprite(SpriteParams{Path: "icon.png"})
	element := NewElement().Visual(visual, params)
	if got := visualTypeName(element.visual); got != "ui.spriteVisual" {
		t.Errorf("sprite visual = %q, want ui.spriteVisual", got)
	}
	interactive, payload := InteractiveColor(InteractiveColorParams{})
	element = NewElement().Visual(interactive, payload)
	if got := visualTypeName(element.visual); got != "ui.interactiveColorVisual" {
		t.Errorf("interactive colour visual = %q, want ui.interactiveColorVisual", got)
	}
	if got := goTypeName(&[]int{}); got != "*[]int" {
		t.Errorf("a type from nowhere = %q, want *[]int", got)
	}
	if got := goTypeName(t); got != "*testing.T" {
		t.Errorf("a type from another package = %q, want *testing.T", got)
	}
}
