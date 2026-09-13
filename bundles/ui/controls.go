package ui

type ButtonParams struct {
	ID       ID
	Disabled bool
}

func Button(params ButtonParams) Element {
	element := Vertical().ChildrenAlignment(AlignStretch).ChildrenArrangement(ArrangeCenter)
	if params.ID != "" {
		element = element.ID(params.ID)
	}
	if params.Disabled {
		element = element.State(VisualDisabled, 0)
	}
	return element
}
