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

func Spacer() Element {
	return NewElement().Stretch(1)
}
