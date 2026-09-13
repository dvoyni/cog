package ui

import "github.com/dvoyni/cog/bundles/ui/internal"

// Overlay returns a container whose children share the same layout area.
func Overlay(children ...Element) Element { return internal.Overlay(children...) }

// Horizontal returns a container that lays out its children from left to right.
func Horizontal(children ...Element) Element { return internal.Horizontal(children...) }

// Vertical returns a container that lays out its children from top to bottom.
func Vertical(children ...Element) Element { return internal.Vertical(children...) }

// Grid returns a container that lays out its children in a grid.
func Grid(children ...Element) Element { return internal.Grid(children...) }

// WithFloating pairs an anchor with an element that hangs off it — a tooltip, a
// dropdown, a badge. The floating element is added to the anchor rather than the
// two being wrapped together, so the result *is* the anchor and takes the
// anchor's place in the surrounding layout unchanged.
//
// The floating element ignores layout, so it never resizes its anchor, and
// every layout arranges it against the anchor's rect. It ignores clip so it can
// hang outside that rect, and it is cut loose from the anchor's interaction
// state, so a tooltip on a hovered button is not painted in the button's hover
// tint. A caller wanting the anchor's state back can add it with State.
//
// Hit testing is left to the caller. A tooltip wants IgnoreHitTest, so that it
// cannot take the hover keeping it alive; a menu wants to stay clickable.
func WithFloating(anchor, floating Element) Element { return internal.WithFloating(anchor, floating) }

// Spacer returns an empty element with stretch weight 1.
func Spacer() Element { return internal.Spacer() }

// ButtonParams names a Button and says whether it is disabled.
type ButtonParams = internal.ButtonParams

// Button returns a vertical container carrying the interaction policy of a
// button: the requested ID and, when disabled, VisualDisabled. Callers compose
// its background, padding and content.
func Button(params ButtonParams) Element { return internal.Button(params) }
