package ui_test

import (
	"testing"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/ui"
)

type modifierVisual struct {
	states *[]ui.State
}

func (modifierVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	return m.Vec2{X: 10, Y: 10}
}

func (visual modifierVisual) Draw(_ canvas.LookupAccess, _ *canvas.OpQueue, state ui.State, _ any) {
	if visual.states != nil {
		*visual.states = append(*visual.states, state)
	}
}

func TestElementExposesEveryModifier(t *testing.T) {
	children := []ui.Element{ui.NewElement()}
	element := ui.NewElement().
		ID("element").
		Width(100).
		WidthRel(0.5).
		MinWidth(10).
		MinWidthRel(0.1).
		MaxWidth(200).
		MaxWidthRel(1).
		Height(100).
		HeightRel(0.5).
		MinHeight(10).
		MinHeightRel(0.1).
		MaxHeight(200).
		MaxHeightRel(1).
		Left(10).
		LeftRel(0.1).
		Right(10).
		RightRel(0.1).
		Top(10).
		TopRel(0.1).
		Bottom(10).
		BottomRel(0.1).
		PivotLeft(10).
		PivotLeftRel(0.1).
		PivotRight(10).
		PivotRightRel(0.1).
		PivotTop(10).
		PivotTopRel(0.1).
		PivotBottom(10).
		PivotBottomRel(0.1).
		Padding(1, 2, 3, 4, 5).
		PaddingRel(0.1, 0.2).
		PaddingLeft(1).
		PaddingLeftRel(0.1).
		PaddingRight(1).
		PaddingRightRel(0.1).
		PaddingTop(1).
		PaddingTopRel(0.1).
		PaddingBottom(1).
		PaddingBottomRel(0.1).
		Stretch(1).
		Shrink(1).
		Align(ui.AlignCenter).
		Layer(1).
		IgnoreLayout().
		IgnoreClip().
		State(ui.VisualActive, ui.VisualDisabled).
		Children(children...).
		Layout(ui.LayoutGrid).
		ChildrenArrangement(ui.ArrangeCenter).
		ChildrenAlignment(ui.AlignCenter).
		Gap(8).
		GapRel(0.1).
		Wrap().
		Columns(1).
		Rows(1).
		Visual(modifierVisual{}, nil)
	_ = element
}

var modifierSink ui.Element

func TestElementModifiersDoNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() {
		modifierSink = ui.NewElement().
			ID("element").
			WidthRel(0.5).
			Height(20).
			Left(10).
			Stretch(1).
			Visual(modifierVisual{}, nil)
	})
	if allocations != 0 {
		t.Fatalf("modifier allocations = %v, want 0", allocations)
	}
}
