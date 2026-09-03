package ui_test

import (
	"testing"

	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/ui"
)

func TestContainerConstructorsSelectLayout(t *testing.T) {
	children := []ui.Element{
		ui.NewElement().Width(10).Height(20),
		ui.NewElement().Width(30).Height(5),
	}
	tests := []struct {
		name    string
		element ui.Element
		want    m.Vec2
	}{
		{name: "overlay", element: ui.Overlay(children...), want: m.Vec2{X: 30, Y: 20}},
		{name: "horizontal", element: ui.Horizontal(children...), want: m.Vec2{X: 40, Y: 20}},
		{name: "vertical", element: ui.Vertical(children...), want: m.Vec2{X: 30, Y: 25}},
		{name: "grid", element: ui.Grid(children...).Columns(2), want: m.Vec2{X: 40, Y: 20}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ui.Measure(test.element, m.Vec2{X: 100, Y: 100}); got != test.want {
				t.Fatalf("Measure() = %+v, want %+v", got, test.want)
			}
		})
	}
}

// A wrapper that stands in for its anchor must measure to the anchor alone, or
// an element that appears only on hover would resize its own anchor and move it
// out from under the pointer.
func TestWithFloatingMeasuresToAnchorOnly(t *testing.T) {
	measured := ui.Measure(
		ui.WithFloating(ui.NewElement().Width(30).Height(20), ui.NewElement().Width(200).Height(80)),
		m.Vec2{X: 500, Y: 500},
	)
	if measured != (m.Vec2{X: 30, Y: 20}) {
		t.Fatalf("measured = %+v, want the anchor's size {X:30 Y:20}", measured)
	}
}
