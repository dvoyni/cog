package ui

// Overlay returns a container whose children share the same layout area.
func Overlay(children ...Element) Element {
	return NewElement().Layout(LayoutNone).Children(children...)
}

// Horizontal returns a container that lays out its children from left to right.
func Horizontal(children ...Element) Element {
	return NewElement().Layout(LayoutHorizontal).Children(children...)
}

// Vertical returns a container that lays out its children from top to bottom.
func Vertical(children ...Element) Element {
	return NewElement().Layout(LayoutVertical).Children(children...)
}

// Grid returns a container that lays out its children in a grid.
func Grid(children ...Element) Element {
	return NewElement().Layout(LayoutGrid).Children(children...)
}

// WithFloating pairs an anchor with an element that hangs off it — a tooltip, a
// dropdown, a badge. The anchor stands in for the pair in the surrounding
// layout, so size and position the result rather than the anchor; the anchor's
// own edges are overwritten, which would clobber one that positions itself.
//
// The wrapper measures to the anchor alone, so wrapping an element changes
// nothing about the layout around it. Two details make that work, and both are
// easy to get wrong. The anchor is pinned with pixel edges rather than Fill:
// Fill sets relative edges, and a LayoutNone parent contributes zero for a
// child sized relative to itself, so the wrapper would collapse to nothing. And
// the floating element ignores layout, which keeps it out of the wrapper's
// measurement — otherwise an element that appears on hover would resize its own
// anchor, moving it out from under the pointer.
//
// Hit testing is left to the caller. A tooltip wants IgnoreHitTest, so that it
// cannot take the hover keeping it alive; a menu wants to stay clickable.
func WithFloating(anchor, floating Element) Element {
	return Overlay(
		anchor.Left(0).Right(0).Top(0).Bottom(0),
		floating.IgnoreLayout().IgnoreClip(),
	)
}

func Spacer() Element {
	return NewElement().Stretch(1)
}
