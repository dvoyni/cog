package ui

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

type recordingVisual struct {
	defaultSize  m.Vec2
	states       []State
	order        *[]ID
	id           ID
	defaultCalls int
}

func (visual *recordingVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	visual.defaultCalls++
	return visual.defaultSize
}

func (visual *recordingVisual) Draw(_ canvas.LookupAccess, _ *canvas.OpQueue, state State, _ any) {
	visual.states = append(visual.states, state)
	if visual.order != nil {
		*visual.order = append(*visual.order, visual.id)
	}
}

func TestProcessResolvesPivotsAndClipsChildren(t *testing.T) {
	childVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	rootVisual := &recordingVisual{}
	child := NewElement().
		Left(90).
		Visual(childVisual, nil)
	root := NewElement().
		Width(100).
		Height(80).
		Left(20).
		Top(30).
		PivotLeft(10).
		PivotTop(5).
		Visual(rootVisual, nil).
		Children(child)
	roots := []Element{root}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 200, Height: 200}}, nil)

	assertRect(t, rootVisual.states[0].Rect, Rect{X: 10, Y: 25, Width: 100, Height: 80})
	assertRect(t, childVisual.states[0].Rect, Rect{X: 100, Y: 25, Width: 20, Height: 10})
	assertRect(t, childVisual.states[0].ClipRect, Rect{X: 10, Y: 25, Width: 100, Height: 80})
	if childVisual.defaultCalls != 1 {
		t.Fatalf("DefaultSize calls = %d, want 1", childVisual.defaultCalls)
	}
}

func TestProcessDistributesHorizontalStretch(t *testing.T) {
	leftVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	rightVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	children := []Element{
		NewElement().Stretch(1).Visual(leftVisual, nil),
		NewElement().Stretch(3).Visual(rightVisual, nil),
	}
	roots := []Element{Horizontal().
		Width(100).
		Height(20).
		ChildrenAlignment(AlignCenter).
		Children(children...)}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 100, Height: 20}}, nil)

	assertRect(t, leftVisual.states[0].Rect, Rect{X: 0, Y: 5, Width: 30, Height: 10})
	assertRect(t, rightVisual.states[0].Rect, Rect{X: 30, Y: 5, Width: 70, Height: 10})
}

func TestProcessAppliesPaddingGapAndCrossAxisStretch(t *testing.T) {
	rootVisual := &recordingVisual{}
	leftVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	rightVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	children := []Element{
		NewElement().Visual(leftVisual, nil).PreserveAspectRatio(),
		NewElement().Visual(rightVisual, nil).PreserveAspectRatio(),
	}
	roots := []Element{Horizontal().
		Width(100).
		Height(40).
		Padding(5, 10).
		GapRel(0.1).
		ChildrenAlignment(AlignStretch).
		Visual(rootVisual, nil).
		Children(children...)}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 100, Height: 40}}, nil)

	assertRect(t, rootVisual.states[0].ContentRect, Rect{X: 10, Y: 5, Width: 80, Height: 30})
	assertRect(t, leftVisual.states[0].Rect, Rect{X: 10, Y: 5, Width: 30, Height: 30})
	assertRect(t, rightVisual.states[0].Rect, Rect{X: 48, Y: 5, Width: 30, Height: 30})
}

func TestProcessDefiniteCrossAxisStretchPreservesVisualAspectRatio(t *testing.T) {
	t.Run("horizontal", func(t *testing.T) {
		visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
		root := Horizontal().
			Width(200).
			Height(30).
			ChildrenAlignment(AlignStretch).
			Children(NewElement().Visual(visual, nil).PreserveAspectRatio())

		var context processor
		context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 30}}, nil)

		assertRect(t, visual.states[0].Rect, Rect{Width: 60, Height: 30})
	})

	t.Run("vertical", func(t *testing.T) {
		visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
		root := Vertical().
			Width(30).
			Height(200).
			ChildrenAlignment(AlignStretch).
			Children(NewElement().Visual(visual, nil).PreserveAspectRatio())

		var context processor
		context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 30, Height: 200}}, nil)

		assertRect(t, visual.states[0].Rect, Rect{Width: 30, Height: 15})
	})
}

func TestProcessIntrinsicCrossAxisStretchKeepsVisualIntrinsicSize(t *testing.T) {
	visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	root := Horizontal().
		ChildrenAlignment(AlignStretch).
		Children(NewElement().Visual(visual, nil).PreserveAspectRatio())

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 100}}, nil)

	assertRect(t, visual.states[0].Rect, Rect{Width: 40, Height: 20})
}

func TestProcessDefiniteCrossAxisStretchDoesNotPreserveAspectRatioByDefault(t *testing.T) {
	visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	root := Horizontal().
		Width(200).
		Height(30).
		ChildrenAlignment(AlignStretch).
		Children(NewElement().Visual(visual, nil))

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 30}}, nil)

	assertRect(t, visual.states[0].Rect, Rect{Width: 40, Height: 30})
}

func TestProcessDefiniteStretchRespectsExplicitMainAxis(t *testing.T) {
	visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	root := Horizontal().
		Width(200).
		Height(30).
		ChildrenAlignment(AlignStretch).
		Children(NewElement().Width(25).Visual(visual, nil).PreserveAspectRatio())

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 30}}, nil)

	assertRect(t, visual.states[0].Rect, Rect{Width: 25, Height: 30})
}

// A child that takes its cross axis from the row measures as zero on both axes,
// and so its ratio gives it no main axis either. Left at that the row measures
// to its other children alone and then arranges wider than it measured, pushing
// the last of its content past its own edge to be clipped there.
func TestProcessRowReservesWidthForRelativeHeightAspectChild(t *testing.T) {
	rowVisual := &recordingVisual{}
	iconVisual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	textVisual := &recordingVisual{defaultSize: m.Vec2{X: 300, Y: 23}}
	root := Horizontal(
		NewElement().Visual(iconVisual, nil).PreserveAspectRatio().HeightRel(1),
		NewElement().Visual(textVisual, nil),
	).Gap(8).Visual(rowVisual, nil)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 800, Height: 600}}, nil)

	assertRect(t, rowVisual.states[0].Rect, Rect{Width: 354, Height: 23})
	assertRect(t, iconVisual.states[0].Rect, Rect{Width: 46, Height: 23})
	assertRect(t, textVisual.states[0].Rect, Rect{X: 54, Width: 300, Height: 23})
}

func TestProcessColumnReservesHeightForRelativeWidthAspectChild(t *testing.T) {
	columnVisual := &recordingVisual{}
	iconVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 40}}
	textVisual := &recordingVisual{defaultSize: m.Vec2{X: 23, Y: 300}}
	root := Vertical(
		NewElement().Visual(iconVisual, nil).PreserveAspectRatio().WidthRel(1),
		NewElement().Visual(textVisual, nil),
	).Gap(8).Visual(columnVisual, nil)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 800, Height: 600}}, nil)

	assertRect(t, columnVisual.states[0].Rect, Rect{Width: 23, Height: 354})
	assertRect(t, iconVisual.states[0].Rect, Rect{Width: 23, Height: 46})
	assertRect(t, textVisual.states[0].Rect, Rect{Y: 54, Width: 23, Height: 300})
}

// Where the row has a cross axis of its own, that is what the child resolves
// against, padding taken off it as arrangement will take it off.
func TestProcessRelativeCrossChildMeasuresAgainstOwnRowHeight(t *testing.T) {
	rowVisual := &recordingVisual{}
	iconVisual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	root := Horizontal(
		NewElement().Visual(iconVisual, nil).PreserveAspectRatio().HeightRel(1),
	).Height(50).Padding(5, 4).Visual(rowVisual, nil)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 800, Height: 600}}, nil)

	assertRect(t, rowVisual.states[0].Rect, Rect{Width: 88, Height: 50})
	assertRect(t, iconVisual.states[0].Rect, Rect{X: 4, Y: 5, Width: 80, Height: 40})
}

func TestLayoutNoneFillUsesContentUnlessChildIgnoresLayout(t *testing.T) {
	contentVisual := &recordingVisual{}
	parentVisual := &recordingVisual{}
	root := NewElement().
		Width(100).
		Height(40).
		Padding(5, 10).
		Children(
			NewElement().Fill().Visual(contentVisual, nil),
			NewElement().Fill().IgnoreLayout().Visual(parentVisual, nil),
		)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 40}}, nil)

	assertRect(t, contentVisual.states[0].Rect, Rect{X: 10, Y: 5, Width: 80, Height: 30})
	assertRect(t, parentVisual.states[0].Rect, Rect{Width: 100, Height: 40})
}

func TestProcessNegativeGapOverlapsChildren(t *testing.T) {
	leftVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	rightVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	children := []Element{
		NewElement().Visual(leftVisual, nil),
		NewElement().Visual(rightVisual, nil),
	}
	rootVisual := &recordingVisual{}
	root := Horizontal().
		Gap(-2).
		Visual(rootVisual, nil).
		Children(children...)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	assertRect(t, rootVisual.states[0].Rect, Rect{Width: 18, Height: 10})
	assertRect(t, leftVisual.states[0].Rect, Rect{Width: 10, Height: 10})
	assertRect(t, rightVisual.states[0].Rect, Rect{X: 8, Width: 10, Height: 10})
}

func TestProcessAddsPaddingToVisualIntrinsicSize(t *testing.T) {
	visual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	roots := []Element{NewElement().Padding(1, 2, 3, 4).Visual(visual, nil)}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	assertRect(t, visual.states[0].Rect, Rect{Width: 26, Height: 14})
	assertRect(t, visual.states[0].ContentRect, Rect{X: 4, Y: 1, Width: 20, Height: 10})
}

func TestProcessPreservesVisualAspectRatioWithOneSpecifiedAxis(t *testing.T) {
	tests := []struct {
		name    string
		element Element
		want    Rect
	}{
		{name: "width", element: NewElement().Width(80), want: Rect{Width: 80, Height: 40}},
		{name: "height", element: NewElement().Height(30), want: Rect{Width: 60, Height: 30}},
		{name: "relative width", element: NewElement().WidthRel(0.5), want: Rect{Width: 100, Height: 50}},
		{name: "relative height", element: NewElement().HeightRel(0.5), want: Rect{Width: 120, Height: 60}},
		{name: "both specified", element: NewElement().Width(80).Height(30), want: Rect{Width: 80, Height: 30}},
		{name: "neither specified", element: NewElement(), want: Rect{Width: 40, Height: 20}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
			element := test.element.Visual(visual, nil).PreserveAspectRatio()

			var context processor
			context.process(canvas.LookupAccess{}, []Element{element}, nil, globalState{Screen: Rect{Width: 200, Height: 120}}, nil)

			assertRect(t, visual.states[0].Rect, test.want)
		})
	}
}

func TestOverlayButtonKeepsBackgroundAndBorderOutsideLabelPadding(t *testing.T) {
	backgroundVisual := &recordingVisual{}
	borderVisual := &recordingVisual{}
	labelVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	children := []Element{
		NewElement().WidthRel(1).HeightRel(1).Visual(backgroundVisual, nil),
		NewElement().WidthRel(1).HeightRel(1).Visual(borderVisual, nil),
		NewElement().Left(0).Right(0).Top(0).Bottom(0).Padding(8, 16).Visual(labelVisual, nil),
	}
	root := Button(ButtonParams{ID: "button"}).Width(100).Height(40).Children(children...)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	wantOuter := Rect{Width: 100, Height: 40}
	assertRect(t, backgroundVisual.states[0].Rect, wantOuter)
	assertRect(t, borderVisual.states[0].Rect, wantOuter)
	assertRect(t, labelVisual.states[0].Rect, wantOuter)
	assertRect(t, labelVisual.states[0].ContentRect, Rect{X: 16, Y: 8, Width: 68, Height: 24})
}

func TestProcessMeasuresAbsoluteChildrenFromPixelAnchors(t *testing.T) {
	parentVisual := &recordingVisual{}
	childVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	child := NewElement().Left(5).Right(7).Top(3).Bottom(4).Visual(childVisual, nil)
	roots := []Element{NewElement().Visual(parentVisual, nil).Children(child)}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	assertRect(t, parentVisual.states[0].Rect, Rect{Width: 32, Height: 17})
	assertRect(t, childVisual.states[0].Rect, Rect{X: 5, Y: 3, Width: 20, Height: 10})
}

func TestProcessSupportsNegativeEdgesAndLayer(t *testing.T) {
	childVisual := &recordingVisual{}
	child := NewElement().
		Left(-10).Right(-20).Top(-5).Bottom(-15).
		Layer(-1).
		Visual(childVisual, nil)
	root := NewElement().Width(100).Height(80).Children(child)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, []canvas.Layer{10}, globalState{Screen: Rect{Width: 100, Height: 80}}, nil)

	assertRect(t, childVisual.states[0].Rect, Rect{X: -10, Y: -5, Width: 130, Height: 100})
	if got, want := childVisual.states[0].Layer, canvas.Layer(9); got != want {
		t.Fatalf("child layer = %d, want %d", got, want)
	}
}

func TestProcessEdgeFillMatchesDimensionFillInIntrinsicParent(t *testing.T) {
	fillers := []struct {
		name string
		fill func(Element) Element
	}{
		{name: "edges", fill: func(element Element) Element {
			return element.Fill()
		}},
		{name: "dimensions", fill: func(element Element) Element {
			return element.LeftRel(0).WidthRel(1).TopRel(0).HeightRel(1)
		}},
	}

	for _, filler := range fillers {
		t.Run(filler.name, func(t *testing.T) {
			parentVisual := &recordingVisual{}
			backgroundVisual := &recordingVisual{defaultSize: m.Vec2{X: 100, Y: 50}}
			contentVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
			background := filler.fill(NewElement()).Visual(backgroundVisual, nil)
			root := NewElement().Visual(parentVisual, nil).Children(
				background,
				NewElement().Visual(contentVisual, nil),
			)

			var context processor
			context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 200}}, nil)

			assertRect(t, parentVisual.states[0].Rect, Rect{Width: 20, Height: 10})
			assertRect(t, backgroundVisual.states[0].Rect, Rect{Width: 20, Height: 10})
		})
	}
}

func TestMeasureResolvesResponsiveRootSize(t *testing.T) {
	element := Vertical().
		WidthRel(0.5).
		MinWidth(200).
		MaxWidth(400).
		Padding(10).
		Children(NewElement().Width(50).Height(30))

	if got, want := Measure(element, m.Vec2{X: 600, Y: 400}), (m.Vec2{X: 300, Y: 50}); got != want {
		t.Fatalf("Measure() = %+v, want %+v", got, want)
	}
}

func TestProcessInfersSquareGridAndDropsFixedOverflow(t *testing.T) {
	visuals := make([]*recordingVisual, 5)
	children := make([]Element, len(visuals))
	for index := range visuals {
		visuals[index] = &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
		children[index] = NewElement().Visual(visuals[index], nil)
	}
	roots := []Element{Grid(children...)}

	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	want := []Rect{
		{X: 0, Y: 0, Width: 10, Height: 10},
		{X: 10, Y: 0, Width: 10, Height: 10},
		{X: 20, Y: 0, Width: 10, Height: 10},
		{X: 0, Y: 10, Width: 10, Height: 10},
		{X: 10, Y: 10, Width: 10, Height: 10},
	}
	for index := range visuals {
		assertRect(t, visuals[index].states[0].Rect, want[index])
	}

	fixedVisuals := make([]*recordingVisual, 3)
	fixedChildren := make([]Element, len(fixedVisuals))
	for index := range fixedVisuals {
		fixedVisuals[index] = &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
		fixedChildren[index] = NewElement().Visual(fixedVisuals[index], nil)
	}
	fixed := []Element{Grid().
		Columns(1).
		Rows(2).
		Children(fixedChildren...)}
	context.process(canvas.LookupAccess{}, fixed, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)
	if len(fixedVisuals[0].states) != 1 || len(fixedVisuals[1].states) != 1 || len(fixedVisuals[2].states) != 0 {
		t.Fatalf("fixed grid draw counts = %d, %d, %d; want 1, 1, 0",
			len(fixedVisuals[0].states), len(fixedVisuals[1].states), len(fixedVisuals[2].states))
	}
}

func TestProcessGridSizesEachTrackFromItsLargestElement(t *testing.T) {
	visuals := []*recordingVisual{
		{defaultSize: m.Vec2{X: 10, Y: 5}},
		{defaultSize: m.Vec2{X: 20, Y: 7}},
		{defaultSize: m.Vec2{X: 30, Y: 11}},
		{defaultSize: m.Vec2{X: 15, Y: 13}},
	}
	children := make([]Element, len(visuals))
	for index := range visuals {
		children[index] = NewElement().Visual(visuals[index], nil)
	}
	rootVisual := &recordingVisual{}
	root := Grid().
		Columns(2).
		Rows(2).
		Gap(2).
		ChildrenAlignment(AlignStretch).
		Visual(rootVisual, nil).
		Children(children...)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 100}}, nil)

	assertRect(t, rootVisual.states[0].Rect, Rect{Width: 52, Height: 22})
	assertRect(t, visuals[0].states[0].Rect, Rect{Width: 30, Height: 7})
	assertRect(t, visuals[1].states[0].Rect, Rect{X: 32, Width: 20, Height: 7})
	assertRect(t, visuals[2].states[0].Rect, Rect{Y: 9, Width: 30, Height: 13})
	assertRect(t, visuals[3].states[0].Rect, Rect{X: 32, Y: 9, Width: 20, Height: 13})
}

func TestProcessGridExpandsTracksToDefiniteSize(t *testing.T) {
	visuals := []*recordingVisual{
		{defaultSize: m.Vec2{X: 10, Y: 5}},
		{defaultSize: m.Vec2{X: 20, Y: 7}},
		{defaultSize: m.Vec2{X: 30, Y: 11}},
		{defaultSize: m.Vec2{X: 15, Y: 13}},
	}
	children := make([]Element, len(visuals))
	for index := range visuals {
		children[index] = NewElement().Visual(visuals[index], nil)
	}
	root := Grid().
		Columns(2).
		Rows(2).
		Gap(2).
		Width(100).
		Height(60).
		ChildrenArrangement(ArrangeCenter).
		ChildrenAlignment(AlignCenter).
		Children(children...)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 60}}, nil)

	assertRect(t, visuals[0].states[0].Rect, Rect{X: 22, Y: 10.5, Width: 10, Height: 5})
	assertRect(t, visuals[1].states[0].Rect, Rect{X: 68, Y: 9.5, Width: 20, Height: 7})
	assertRect(t, visuals[2].states[0].Rect, Rect{X: 12, Y: 38.5, Width: 30, Height: 11})
	assertRect(t, visuals[3].states[0].Rect, Rect{X: 70.5, Y: 37.5, Width: 15, Height: 13})
}

func TestProcessGivesOverlappingInteractionsToTopmostOnly(t *testing.T) {
	var drawOrder []ID
	lowVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}, order: &drawOrder, id: "low"}
	highVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}, order: &drawOrder, id: "high"}
	roots := []Element{
		NewElement().ID("low").Visual(lowVisual, nil),
		NewElement().ID("high").Visual(highVisual, nil),
	}
	layers := []canvas.Layer{3, 7}
	state := globalState{
		Screen:  Rect{Width: 100, Height: 100},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	if !reflect.DeepEqual(drawOrder, []ID{"low", "high"}) {
		t.Fatalf("draw order = %v, want [low high]", drawOrder)
	}
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "high", Kind: InteractionDown, Button: 0},
		{ID: "high", Kind: InteractionIn, Button: -1},
		{ID: "high", Kind: InteractionHover, Button: -1},
	})
	if !highVisual.states[0].Has(VisualPressed) {
		t.Fatal("topmost element hit on down must be pressed")
	}
	if lowVisual.states[0].Has(VisualPressed) {
		t.Fatal("element under the topmost one must not be pressed")
	}

	drawOrder = drawOrder[:0]
	state.Pointer.Events = []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventUp}}
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "high", Kind: InteractionUp, Button: 0},
		{ID: "high", Kind: InteractionClick, Button: 0},
		{ID: "high", Kind: InteractionHover, Button: -1},
	})
}

func TestProcessIgnoreHitTestFallsThroughToElementBelow(t *testing.T) {
	belowVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}}
	overlayVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}}
	closeVisual := &recordingVisual{defaultSize: m.Vec2{X: 5, Y: 5}}
	roots := []Element{
		NewElement().ID("below").Visual(belowVisual, nil),
		NewElement().IgnoreHitTest().Visual(overlayVisual, nil).Children(
			NewElement().ID("close").Left(0).Top(0).Width(5).Height(5).Visual(closeVisual, nil),
		),
	}
	layers := []canvas.Layer{0, 1}
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 10, Y: 10, Events: []pointerEvent{{X: 10, Y: 10, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "below", Kind: InteractionDown, Button: 0},
		{ID: "below", Kind: InteractionIn, Button: -1},
		{ID: "below", Kind: InteractionHover, Button: -1},
	})

	state.Pointer.X, state.Pointer.Y = 2, 2
	state.Pointer.Events = []pointerEvent{{X: 2, Y: 2, Button: 0, Kind: pointerEventDown}}
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "close", Kind: InteractionDown, Button: 0},
		{ID: "close", Kind: InteractionIn, Button: -1},
		{ID: "close", Kind: InteractionHover, Button: -1},
		{ID: "below", Kind: InteractionOut, Button: -1},
	})
}

func TestProcessReturnsCapturedElementUserData(t *testing.T) {
	root := NewElement().ID("target").UserData("unit data").Width(20).Height(20)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, state, nil)
	state.Pointer.Events = []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventUp}}
	context.process(canvas.LookupAccess{}, []Element{root.UserData("changed data")}, nil, state, nil)

	interactions := Interactions{values: context.interactions}
	found, userData := interactions.Has("target", InteractionClick, 0, false)
	if !found || userData != "unit data" {
		t.Fatalf("Has result = %v, %q; want true, %q", found, userData, "unit data")
	}
}

func TestProcessGivesNestedInteractionsToInnermostID(t *testing.T) {
	child := NewElement().ID("child").Width(20).Height(20)
	parent := NewElement().ID("parent").Width(20).Height(20).Children(child)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{parent}, nil, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "child", Kind: InteractionDown, Button: 0},
		{ID: "child", Kind: InteractionIn, Button: -1},
		{ID: "child", Kind: InteractionHover, Button: -1},
	})
}

func TestAnonymousElementDerivesInteractionToNearestIDedAncestor(t *testing.T) {
	leaf := NewElement().Width(20).Height(20)
	anonymous := NewElement().Width(20).Height(20).Children(leaf)
	parent := NewElement().ID("parent").Width(20).Height(20).Children(anonymous)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{parent}, nil, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "parent", Kind: InteractionDown, Button: 0},
		{ID: "parent", Kind: InteractionIn, Button: -1},
		{ID: "parent", Kind: InteractionHover, Button: -1},
	})
}

func TestAnonymousElementWithoutIDedAncestorBlocksEverythingBelow(t *testing.T) {
	button := NewElement().ID("button").Fill()
	shade := NewElement().Fill().Children(NewElement().Fill())
	roots := []Element{button, shade}
	layers := []canvas.Layer{1, 2}
	state := globalState{
		Screen: Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{
			{X: 5, Y: 5, Button: 0, Kind: pointerEventDown},
			{X: 5, Y: 5, Button: 0, Kind: pointerEventUp},
		}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	if len(context.interactions) != 0 {
		t.Fatalf("blocked interactions = %+v, want none", context.interactions)
	}
}

func TestProcessUsesLayerBeforeNestingForInteractionOrder(t *testing.T) {
	child := NewElement().ID("child").Layer(-1).Width(20).Height(20)
	parent := NewElement().ID("parent").Width(20).Height(20).Children(child)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{parent}, nil, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "parent", Kind: InteractionDown, Button: 0},
		{ID: "parent", Kind: InteractionIn, Button: -1},
		{ID: "parent", Kind: InteractionHover, Button: -1},
	})
}

func TestAnonymousChildrenInheritInteractiveContainerState(t *testing.T) {
	childVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	child := NewElement().Visual(childVisual, nil)
	root := Button(ButtonParams{ID: "button"}).
		Width(20).
		Height(20).
		Children(child)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, state, nil)
	if len(childVisual.states) != 1 || !childVisual.states[0].Has(VisualHovered|VisualPressed) {
		t.Fatalf("child state = %+v, want hovered and pressed", childVisual.states)
	}
}

func TestVisualStateTransformsPropagateThroughSubtree(t *testing.T) {
	const selected VisualState = VisualUserDefinedBase
	parentVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}}
	childVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	child := NewElement().State(selected, VisualActive).Visual(childVisual, nil)
	parent := NewElement().
		State(VisualActive, 0).
		Visual(parentVisual, nil).
		Children(child)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{parent}, nil, globalState{Screen: Rect{Width: 20, Height: 20}}, nil)
	if !parentVisual.states[0].Has(VisualActive) {
		t.Fatalf("parent state = %v, want active", parentVisual.states[0].VisualState)
	}
	if got := childVisual.states[0].VisualState; got != selected {
		t.Fatalf("child state = %v, want selected only", got)
	}
}

func TestVisualStateAdditionWinsOverRemoval(t *testing.T) {
	visual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	root := NewElement().State(VisualActive, VisualActive).Visual(visual, nil)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 10, Height: 10}}, nil)
	if !visual.states[0].Has(VisualActive) {
		t.Fatalf("state = %v, want active", visual.states[0].VisualState)
	}
}

func TestDisabledElementSuppressesHoverDownAndClick(t *testing.T) {
	root := Button(ButtonParams{ID: "button", Disabled: true}).Width(20).Height(20)
	state := globalState{
		Screen: Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{
			{X: 5, Y: 5, Button: 0, Kind: pointerEventDown},
			{X: 5, Y: 5, Button: 0, Kind: pointerEventUp},
		}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, state, nil)
	if len(context.interactions) != 0 {
		t.Fatalf("disabled interactions = %+v, want none", context.interactions)
	}
}

func TestCaptureReleasedAfterElementBecomesDisabled(t *testing.T) {
	enabled := Button(ButtonParams{ID: "button"}).Width(20).Height(20)
	disabled := Button(ButtonParams{ID: "button", Disabled: true}).Width(20).Height(20)
	state := globalState{
		Screen:  Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, []Element{enabled}, nil, state, nil)
	state.Pointer.Events = []pointerEvent{{X: 5, Y: 5, Button: 0, Kind: pointerEventUp}}
	context.process(canvas.LookupAccess{}, []Element{disabled}, nil, state, nil)
	assertInteractions(t, context.interactions, []Interaction{
		{ID: "button", Kind: InteractionUp, Button: 0},
		{ID: "button", Kind: InteractionOut, Button: -1},
	})
}

func TestCoveringElementBlocksElementsBelowIt(t *testing.T) {
	button := NewElement().ID("button").Width(20).Height(20)
	shade := NewElement().ID("shade").Fill()
	roots := []Element{button, shade}
	layers := []canvas.Layer{1, 2}
	state := globalState{
		Screen: Rect{Width: 20, Height: 20},
		Pointer: pointerState{X: 5, Y: 5, Events: []pointerEvent{
			{X: 5, Y: 5, Button: 0, Kind: pointerEventDown},
			{X: 5, Y: 5, Button: 0, Kind: pointerEventUp},
		}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	if hasInteraction(context.interactions, "button", InteractionDown) ||
		hasInteraction(context.interactions, "button", InteractionClick) ||
		hasInteraction(context.interactions, "button", InteractionHover) {
		t.Fatalf("blocked button interactions = %+v, want none", context.interactions)
	}
	if !hasInteraction(context.interactions, "shade", InteractionClick) {
		t.Fatalf("shade interactions = %+v, want click", context.interactions)
	}
}

func TestCoveringElementOnlyBlocksInsideItsRect(t *testing.T) {
	button := NewElement().ID("button").Fill()
	shade := NewElement().ID("shade").Width(10).Height(10)
	roots := []Element{button, shade}
	layers := []canvas.Layer{1, 2}
	state := globalState{
		Screen:  Rect{Width: 40, Height: 40},
		Pointer: pointerState{X: 25, Y: 25, Events: []pointerEvent{{X: 25, Y: 25, Button: 0, Kind: pointerEventDown}}},
	}

	var context processor
	context.process(canvas.LookupAccess{}, roots, layers, state, nil)
	if !hasInteraction(context.interactions, "button", InteractionDown) {
		t.Fatalf("interactions = %+v, want button down outside the consuming rect", context.interactions)
	}
}

func TestIgnoreClipLetsPopupEscapeParent(t *testing.T) {
	clippedVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	popupVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	clippedChild := NewElement().ID("child").Left(8).Visual(clippedVisual, nil)
	clipped := []Element{NewElement().Width(10).Height(10).Children(clippedChild)}
	popupChild := NewElement().ID("popup").Left(8).IgnoreClip().Visual(popupVisual, nil)
	popup := []Element{NewElement().Width(10).Height(10).Children(popupChild)}
	state := globalState{Screen: Rect{Width: 100, Height: 100}, Pointer: pointerState{X: 12, Y: 5}}

	var context processor
	context.process(canvas.LookupAccess{}, clipped, nil, state, nil)
	if hasInteraction(context.interactions, "child", InteractionHover) {
		t.Fatal("clipped child unexpectedly received hover")
	}
	context.process(canvas.LookupAccess{}, popup, nil, state, nil)
	if !hasInteraction(context.interactions, "popup", InteractionHover) {
		t.Fatal("popup did not receive hover outside its parent")
	}
	assertRect(t, popupVisual.states[0].ClipRect, state.Screen)
}

func TestProcessDoesNotAllocateAfterWarmup(t *testing.T) {
	roots := benchmarkTree(1000)
	state := globalState{Screen: Rect{Width: 1000, Height: 1000}}
	var context processor
	context.process(canvas.LookupAccess{}, roots, nil, state, nil)

	allocations := testing.AllocsPerRun(50, func() {
		context.process(canvas.LookupAccess{}, roots, nil, state, nil)
	})
	if allocations != 0 {
		t.Fatalf("allocations per warmed Process = %v, want 0", allocations)
	}
}

func BenchmarkProcess(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(integerName(count), func(b *testing.B) {
			roots := benchmarkTree(count)
			state := globalState{Screen: Rect{Width: 1000, Height: 1000}}
			var context processor
			context.process(canvas.LookupAccess{}, roots, nil, state, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				context.process(canvas.LookupAccess{}, roots, nil, state, nil)
			}
		})
	}
}

type fixedVisual m.Vec2

func (visual fixedVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	return m.Vec2(visual)
}

func (fixedVisual) Draw(canvas.LookupAccess, *canvas.OpQueue, State, any) {}

func benchmarkTree(count int) []Element {
	visual := fixedVisual{X: 10, Y: 10}
	children := make([]Element, count)
	for index := range children {
		children[index] = NewElement().Visual(visual, nil)
	}
	return []Element{Grid().
		Width(1000).
		Height(1000).
		Columns(100).
		Children(children...)}
}

func integerName(value int) string {
	if value == 1000 {
		return "1k"
	}
	return "10k"
}

func TestStayOnScreenClampsOverflowBackInsideViewport(t *testing.T) {
	tests := []struct {
		name    string
		element Element
		want    Rect
	}{
		{name: "past left edge", element: NewElement().Left(-30), want: Rect{X: 0, Y: 0, Width: 40, Height: 20}},
		{name: "past right edge", element: NewElement().Left(90), want: Rect{X: 60, Y: 0, Width: 40, Height: 20}},
		{name: "past bottom edge", element: NewElement().Top(95), want: Rect{X: 0, Y: 30, Width: 40, Height: 20}},
		{name: "already inside", element: NewElement().Left(10).Top(10), want: Rect{X: 10, Y: 10, Width: 40, Height: 20}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			visual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
			root := test.element.StayOnScreen().Visual(visual, nil)

			var context processor
			context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 50}}, nil)

			assertRect(t, visual.states[0].Rect, test.want)
		})
	}
}

func TestStayOnScreenPinsStartEdgeWhenLargerThanViewport(t *testing.T) {
	visual := &recordingVisual{}
	root := NewElement().Left(30).Top(10).Width(160).Height(80).StayOnScreen().Visual(visual, nil)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 50}}, nil)

	assertRect(t, visual.states[0].Rect, Rect{X: 0, Y: 0, Width: 160, Height: 80})
}

func TestStayOnScreenShiftsDescendantsWithIt(t *testing.T) {
	parentVisual := &recordingVisual{}
	childVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	child := NewElement().Left(5).Top(5).Visual(childVisual, nil)
	root := NewElement().Left(-30).Width(40).Height(20).StayOnScreen().
		Visual(parentVisual, nil).Children(child)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 50}}, nil)

	assertRect(t, parentVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 40, Height: 20})
	assertRect(t, childVisual.states[0].Rect, Rect{X: 5, Y: 5, Width: 10, Height: 10})
}

// A clamped element is arranged from its parent's rect rather than taking a
// slot in the parent's flow, so the column neither grows for it nor pushes it
// below its sibling. Without the implied IgnoreLayout it would land at y=10,
// already inside the viewport, and the clamp would never run.
func TestStayOnScreenImpliesIgnoreLayout(t *testing.T) {
	flowVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	pinnedVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	root := Vertical(
		NewElement().Visual(flowVisual, nil),
		NewElement().Top(-40).StayOnScreen().Visual(pinnedVisual, nil),
	)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 100, Height: 50}}, nil)

	assertRect(t, flowVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 20, Height: 10})
	assertRect(t, pinnedVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 20, Height: 10})
}

// A wrapper that stands in for a child in the surrounding layout only works if
// wrapping changes nothing about the wrapper's size. Pixel edges keep the child
// in its parent's measurement; relative edges are resolved from the parent, so
// counting them would be circular and the parent collapses instead.
func TestOverlayMeasuresToChildPinnedWithPixelEdgesOnly(t *testing.T) {
	pinnedVisual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	relativeVisual := &recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}
	root := Horizontal(
		Overlay(NewElement().Left(0).Right(0).Top(0).Bottom(0).Visual(pinnedVisual, nil)),
		Overlay(NewElement().Fill().Visual(relativeVisual, nil)),
	)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 100}}, nil)

	assertRect(t, pinnedVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 40, Height: 20})
	if len(relativeVisual.states) != 0 {
		t.Fatalf("relatively sized child was drawn at %+v, want a collapsed parent", relativeVisual.states[0].Rect)
	}
}

// A child that ignores layout ignores the parent's padding with it, so its
// edges resolve against the parent's rect. Flow and grid parents used to inset
// them by the padding while a LayoutNone parent did not, which made the same
// declaration mean two different things depending on its parent.
func TestIgnoreLayoutChildResolvesAgainstPaddedParentRect(t *testing.T) {
	tests := []struct {
		name string
		root func(children ...Element) Element
	}{
		{name: "flow parent", root: Vertical},
		{name: "grid parent", root: Grid},
		{name: "overlay parent", root: Overlay},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pinnedVisual := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
			root := test.root(
				NewElement().Visual(&recordingVisual{defaultSize: m.Vec2{X: 40, Y: 20}}, nil),
				NewElement().Left(0).Top(0).IgnoreLayout().Visual(pinnedVisual, nil),
			).Padding(8).Left(0).Top(0)

			var context processor
			context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 100}}, nil)

			assertRect(t, pinnedVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 10, Height: 10})
		})
	}
}

// The anchor fills the wrapper, so a row that stretches the pair stretches the
// anchor with it, while the floating element hangs outside the anchor's rect
// and escapes its clip instead of being cut away.
func TestWithFloatingStretchesAnchorAndUnclipsFloating(t *testing.T) {
	anchorVisual := &recordingVisual{defaultSize: m.Vec2{X: 30, Y: 10}}
	floatingVisual := &recordingVisual{defaultSize: m.Vec2{X: 60, Y: 40}}
	root := Horizontal(
		WithFloating(
			NewElement().Visual(anchorVisual, nil),
			NewElement().TopRel(1).Visual(floatingVisual, nil),
		),
	).ChildrenAlignment(AlignStretch).Width(100).Height(50).Left(0).Top(0)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 200}}, nil)

	assertRect(t, anchorVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 30, Height: 50})
	assertRect(t, floatingVisual.states[0].Rect, Rect{X: 0, Y: 50, Width: 60, Height: 40})
}

// Wrapping the pair would stand an element with no art of its own between the
// anchor and the row, and with no art it has no aspect ratio to shrink by: the
// anchor would measure at its authored 512 and shove its neighbour down the row
// the moment it grew a tooltip.
func TestWithFloatingKeepsAnchorAspectRatioInFlow(t *testing.T) {
	plainVisual := &recordingVisual{defaultSize: m.Vec2{X: 512, Y: 512}}
	anchorVisual := &recordingVisual{defaultSize: m.Vec2{X: 512, Y: 512}}
	root := Horizontal(
		NewElement().Visual(plainVisual, nil).PreserveAspectRatio(),
		WithFloating(
			NewElement().Visual(anchorVisual, nil).PreserveAspectRatio(),
			NewElement().TopRel(1).Visual(&recordingVisual{defaultSize: m.Vec2{X: 300, Y: 100}}, nil),
		),
	).ChildrenAlignment(AlignStretch).Width(500).Height(40).Left(0).Top(0)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 600, Height: 600}}, nil)

	assertRect(t, plainVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 40, Height: 40})
	assertRect(t, anchorVisual.states[0].Rect, Rect{X: 40, Y: 0, Width: 40, Height: 40})
}

// The anchor need not be a leaf: a floating element ignores layout, and every
// layout arranges such a child against the parent's rect instead of flowing it
// in, so a row used as an anchor spaces its own children as if it were not there.
func TestWithFloatingUnderAFlowAnchorStaysOutOfTheFlow(t *testing.T) {
	firstVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	secondVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 10}}
	floatingVisual := &recordingVisual{defaultSize: m.Vec2{X: 60, Y: 40}}
	root := WithFloating(
		Horizontal(
			NewElement().Visual(firstVisual, nil),
			NewElement().Visual(secondVisual, nil),
		).Gap(4),
		NewElement().TopRel(1).Visual(floatingVisual, nil),
	).Left(0).Top(0)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 200}}, nil)

	assertRect(t, firstVisual.states[0].Rect, Rect{X: 0, Y: 0, Width: 20, Height: 10})
	assertRect(t, secondVisual.states[0].Rect, Rect{X: 24, Y: 0, Width: 20, Height: 10})
	assertRect(t, floatingVisual.states[0].Rect, Rect{X: 0, Y: 10, Width: 60, Height: 40})
}

// Children inherit their parent's visual state, and the anchor is now the
// floating element's parent. Floating content is a surface of its own, so a
// tooltip on a hovered button must not be painted in the button's hover tint for
// as long as it is up.
func TestWithFloatingDoesNotInheritTheAnchorsState(t *testing.T) {
	anchorVisual := &recordingVisual{defaultSize: m.Vec2{X: 20, Y: 20}}
	floatingVisual := &recordingVisual{defaultSize: m.Vec2{X: 30, Y: 10}}
	root := WithFloating(
		NewElement().State(VisualHovered|VisualActive, 0).Visual(anchorVisual, nil),
		NewElement().TopRel(1).Visual(floatingVisual, nil),
	).Left(0).Top(0)

	var context processor
	context.process(canvas.LookupAccess{}, []Element{root}, nil, globalState{Screen: Rect{Width: 200, Height: 200}}, nil)

	if !anchorVisual.states[0].Has(VisualHovered | VisualActive) {
		t.Fatalf("anchor state = %v, want the state it was given", anchorVisual.states[0].VisualState)
	}
	if got := floatingVisual.states[0].VisualState; got != 0 {
		t.Fatalf("floating state = %v, want none of the anchor's", got)
	}
}

func assertRect(t *testing.T, got, want Rect) {
	t.Helper()
	if got != want {
		t.Fatalf("rect = %+v, want %+v", got, want)
	}
}

func assertInteractions(t *testing.T, got, want []Interaction) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interactions = %+v, want %+v", got, want)
	}
}

func hasInteraction(interactions []Interaction, id ID, kind InteractionKind) bool {
	for index := range interactions {
		if interactions[index].ID == id && interactions[index].Kind == kind {
			return true
		}
	}
	return false
}
