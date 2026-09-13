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
// dropdown, a badge. The floating element is added to the anchor rather than the
// two being wrapped together, so the result *is* the anchor and takes the
// anchor's place in the surrounding layout unchanged.
//
// Wrapping the pair is the obvious implementation and quietly resizes the
// anchor. A wrapper measures to the anchor's measured size, which is not the
// size the anchor would have taken: the wrapper carries none of the anchor's own
// dealings with its parent — its stretch, its alignment, its edges — and, worst
// of the lot, it has no visual and so no aspect ratio. Art authored at 512px
// measures at 512px and counts on its ratio to shrink it once layout fixes its
// height, so under a wrapper it stays 512 wide and shoves its neighbours across
// the row the moment it grows a tooltip.
//
// The floating element ignores layout. That keeps it out of the anchor's
// measurement — otherwise an element that appears on hover would resize its own
// anchor, moving it out from under the pointer — and every layout arranges such
// a child against the anchor's rect instead of flowing it in among the anchor's
// real children, so the anchor can be a row or a grid and not just a leaf. It
// ignores clip so it can hang outside that rect, and it is cut loose from the
// anchor's interaction state: children inherit that state, and floating content
// is a surface of its own, so without this a tooltip on a hovered button would
// sit there painted in the button's hover tint. A caller wanting the anchor's
// state back can add it with State.
//
// Hit testing is left to the caller. A tooltip wants IgnoreHitTest, so that it
// cannot take the hover keeping it alive; a menu wants to stay clickable.
func WithFloating(anchor, floating Element) Element {
	return anchor.Children(
		floating.IgnoreLayout().IgnoreClip().State(0, visualInteractionStates),
	)
}

func Spacer() Element {
	return NewElement().Stretch(1)
}
