package ui

import (
	"github.com/dvoyni/cog/bundles/ui/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// NewElement returns an empty element declaration.
func NewElement() Element { return types.NewElement() }

// Measure resolves element against the available area and returns its arranged
// size without drawing or processing interactions. It has no canvas lookup, so
// intrinsic sprite and text sizes contribute zero.
func Measure(element Element, available m.Vec2) m.Vec2 {
	return types.Measure(element, available)
}

// Overlay returns a container whose children share the same layout area.
func Overlay(children ...Element) Element { return types.Overlay(children...) }

// Horizontal returns a container that lays out its children from left to right.
func Horizontal(children ...Element) Element { return types.Horizontal(children...) }

// Vertical returns a container that lays out its children from top to bottom.
func Vertical(children ...Element) Element { return types.Vertical(children...) }

// Grid returns a container that lays out its children in a grid.
func Grid(children ...Element) Element { return types.Grid(children...) }

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
func WithFloating(anchor, floating Element) Element { return types.WithFloating(anchor, floating) }

// Spacer returns an empty element with stretch weight 1.
func Spacer() Element { return types.Spacer() }

// Button returns a vertical container carrying the interaction policy of a
// button: the requested ID and, when disabled, VisualDisabled. Callers compose
// its background, padding and content.
func Button(params ButtonParams) Element { return types.Button(params) }

// Sprite returns the sprite visual and its params, for Element.Visual.
func Sprite(params SpriteParams) (ParamVisual[SpriteParams], SpriteParams) {
	return types.Sprite(params)
}

// Image returns an element drawing a sprite that preserves its aspect ratio.
func Image(params SpriteParams) Element { return types.Image(params) }

// InteractiveSprite returns the interactive sprite visual and its payload, for
// Element.Visual.
func InteractiveSprite(params InteractiveSpriteParams) (ParamVisual[interactiveSpritePayload], interactiveSpritePayload) {
	return types.InteractiveSprite(params)
}

// InteractiveImage returns an element drawing an interactive sprite that
// preserves its aspect ratio.
func InteractiveImage(params InteractiveSpriteParams) Element {
	return types.InteractiveImage(params)
}

// Sprite9Sliced returns the nine-slice visual and its params, for
// Element.Visual.
func Sprite9Sliced(params Sprite9SlicedParams) (ParamVisual[Sprite9SlicedParams], Sprite9SlicedParams) {
	return types.Sprite9Sliced(params)
}

// Image9Sliced returns an element drawing a nine-slice.
func Image9Sliced(params Sprite9SlicedParams) Element { return types.Image9Sliced(params) }

// InteractiveSprite9Sliced returns the interactive nine-slice visual and its
// payload, for Element.Visual.
func InteractiveSprite9Sliced(params InteractiveSprite9SlicedParams) (ParamVisual[interactiveSprite9SlicedPayload], interactiveSprite9SlicedPayload) {
	return types.InteractiveSprite9Sliced(params)
}

// InteractiveImage9Sliced returns an element drawing an interactive nine-slice.
func InteractiveImage9Sliced(params InteractiveSprite9SlicedParams) Element {
	return types.InteractiveImage9Sliced(params)
}

// Sprite9SliceTiled returns the tiled nine-slice visual and its params, for
// Element.Visual.
func Sprite9SliceTiled(params Sprite9SliceTiledParams) (ParamVisual[Sprite9SliceTiledParams], Sprite9SliceTiledParams) {
	return types.Sprite9SliceTiled(params)
}

// Image9SliceTiled returns an element drawing a tiled nine-slice.
func Image9SliceTiled(params Sprite9SliceTiledParams) Element {
	return types.Image9SliceTiled(params)
}

// InteractiveSprite9SliceTiled returns the interactive tiled nine-slice visual
// and its payload, for Element.Visual.
func InteractiveSprite9SliceTiled(params InteractiveSprite9SliceTiledParams) (ParamVisual[interactiveSprite9SliceTiledPayload], interactiveSprite9SliceTiledPayload) {
	return types.InteractiveSprite9SliceTiled(params)
}

// InteractiveImage9SliceTiled returns an element drawing an interactive tiled
// nine-slice.
func InteractiveImage9SliceTiled(params InteractiveSprite9SliceTiledParams) Element {
	return types.InteractiveImage9SliceTiled(params)
}

// Text returns the text visual and its params, for Element.Visual.
func Text(params TextParams) (ParamVisual[TextParams], TextParams) {
	return types.Text(params)
}

// Label returns an element drawing text.
func Label(params TextParams) Element { return types.Label(params) }

// InteractiveText returns the interactive text visual and its payload, for
// Element.Visual.
func InteractiveText(params InteractiveTextParams) (ParamVisual[interactiveTextPayload], interactiveTextPayload) {
	return types.InteractiveText(params)
}

// InteractiveLabel returns an element drawing interactive text.
func InteractiveLabel(params InteractiveTextParams) Element {
	return types.InteractiveLabel(params)
}

// Color returns the colour-fill visual and its params, for Element.Visual.
func Color(params ColorParams) (ParamVisual[ColorParams], ColorParams) {
	return types.Color(params)
}

// ColorPanel returns an element filled with a solid colour.
func ColorPanel(params ColorParams) Element { return types.ColorPanel(params) }

// InteractiveColor returns the interactive colour-fill visual and its payload,
// for Element.Visual.
func InteractiveColor(params InteractiveColorParams) (ParamVisual[interactiveColorPayload], interactiveColorPayload) {
	return types.InteractiveColor(params)
}

// InteractiveColorPanel returns an element filled with a colour that varies by
// visual state.
func InteractiveColorPanel(params InteractiveColorParams) Element {
	return types.InteractiveColorPanel(params)
}
