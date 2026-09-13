package ui

import "github.com/dvoyni/cog/bundles/ui/internal"

// SpriteFit is how a sprite fills an element whose rect does not match the
// sprite's own ratio.
type SpriteFit = internal.SpriteFit

const (
	SpriteStretch = internal.SpriteStretch
	SpriteContain = internal.SpriteContain
	SpriteCover   = internal.SpriteCover
)

// SpriteParams describes one sprite visual: its path, scale, tint, key colour,
// filter, fit, source frame and rotation.
type SpriteParams = internal.SpriteParams

// VisualStates maps presentation states to the value an interactive visual
// draws with while an element is in them. A key with several bits assigns the
// value to each, and drawing takes the value of the highest active bit.
type VisualStates[T any] = internal.VisualStates[T]

// InteractiveSpriteParams is a sprite whose path and tint vary by visual state.
type InteractiveSpriteParams = internal.InteractiveSpriteParams

// Sprite9SlicedParams describes a nine-slice drawn from one framed texture.
type Sprite9SlicedParams = internal.Sprite9SlicedParams

// InteractiveSprite9SlicedParams is a nine-slice whose path and tint vary by
// visual state.
type InteractiveSprite9SlicedParams = internal.InteractiveSprite9SlicedParams

// Insets are the four edge widths of a tiled nine-slice.
type Insets = internal.Insets

// Sprite9SliceImages names the separate images of a tiled nine-slice.
type Sprite9SliceImages = internal.Sprite9SliceImages

// Sprite9SliceTiledParams describes a nine-slice drawn from separate corner,
// edge and centre images, the edges and centre tiled.
type Sprite9SliceTiledParams = internal.Sprite9SliceTiledParams

// InteractiveSprite9SliceTiledParams is a tiled nine-slice whose images and
// tint vary by visual state.
type InteractiveSprite9SliceTiledParams = internal.InteractiveSprite9SliceTiledParams

// TextAlignment aligns a label's text inside its rect.
type TextAlignment = internal.TextAlignment

const (
	TextAlignStart  = internal.TextAlignStart
	TextAlignCenter = internal.TextAlignCenter
	TextAlignEnd    = internal.TextAlignEnd
)

// Font identifies a font face for UI text: a resource path and a logical pixel
// size. Text is measured and drawn through the canvas Lookup, which parses inline
// ${path} icons in every UI string.
//
// An empty Path draws with the font canvas embeds (canvas.DefaultFontPath), so a
// Font that names only a Size renders rather than disappearing. Size has no
// default: a label with no size is still nothing.
type Font = internal.Font

// TextParams describes one text visual.
type TextParams = internal.TextParams

// InteractiveTextParams is text whose colour varies by visual state.
type InteractiveTextParams = internal.InteractiveTextParams

// ColorParams describes a solid colour fill.
type ColorParams = internal.ColorParams

// InteractiveColorParams is a colour fill that varies by visual state.
type InteractiveColorParams = internal.InteractiveColorParams

// The payloads the interactive visuals bind: the params with their state maps
// packed for drawing. Nothing outside ui names them.
type (
	interactiveSpritePayload            = internal.InteractiveSpritePayload
	interactiveSprite9SlicedPayload     = internal.InteractiveSprite9SlicedPayload
	interactiveSprite9SliceTiledPayload = internal.InteractiveSprite9SliceTiledPayload
	interactiveTextPayload              = internal.InteractiveTextPayload
	interactiveColorPayload             = internal.InteractiveColorPayload
)

// Sprite returns the sprite visual and its params, for Element.Visual.
func Sprite(params SpriteParams) (ParamVisual[SpriteParams], SpriteParams) {
	return internal.Sprite(params)
}

// Image returns an element drawing a sprite that preserves its aspect ratio.
func Image(params SpriteParams) Element { return internal.Image(params) }

// InteractiveSprite returns the interactive sprite visual and its payload, for
// Element.Visual.
func InteractiveSprite(params InteractiveSpriteParams) (ParamVisual[interactiveSpritePayload], interactiveSpritePayload) {
	return internal.InteractiveSprite(params)
}

// InteractiveImage returns an element drawing an interactive sprite that
// preserves its aspect ratio.
func InteractiveImage(params InteractiveSpriteParams) Element {
	return internal.InteractiveImage(params)
}

// Sprite9Sliced returns the nine-slice visual and its params, for
// Element.Visual.
func Sprite9Sliced(params Sprite9SlicedParams) (ParamVisual[Sprite9SlicedParams], Sprite9SlicedParams) {
	return internal.Sprite9Sliced(params)
}

// Image9Sliced returns an element drawing a nine-slice.
func Image9Sliced(params Sprite9SlicedParams) Element { return internal.Image9Sliced(params) }

// InteractiveSprite9Sliced returns the interactive nine-slice visual and its
// payload, for Element.Visual.
func InteractiveSprite9Sliced(params InteractiveSprite9SlicedParams) (ParamVisual[interactiveSprite9SlicedPayload], interactiveSprite9SlicedPayload) {
	return internal.InteractiveSprite9Sliced(params)
}

// InteractiveImage9Sliced returns an element drawing an interactive nine-slice.
func InteractiveImage9Sliced(params InteractiveSprite9SlicedParams) Element {
	return internal.InteractiveImage9Sliced(params)
}

// Sprite9SliceTiled returns the tiled nine-slice visual and its params, for
// Element.Visual.
func Sprite9SliceTiled(params Sprite9SliceTiledParams) (ParamVisual[Sprite9SliceTiledParams], Sprite9SliceTiledParams) {
	return internal.Sprite9SliceTiled(params)
}

// Image9SliceTiled returns an element drawing a tiled nine-slice.
func Image9SliceTiled(params Sprite9SliceTiledParams) Element {
	return internal.Image9SliceTiled(params)
}

// InteractiveSprite9SliceTiled returns the interactive tiled nine-slice visual
// and its payload, for Element.Visual.
func InteractiveSprite9SliceTiled(params InteractiveSprite9SliceTiledParams) (ParamVisual[interactiveSprite9SliceTiledPayload], interactiveSprite9SliceTiledPayload) {
	return internal.InteractiveSprite9SliceTiled(params)
}

// InteractiveImage9SliceTiled returns an element drawing an interactive tiled
// nine-slice.
func InteractiveImage9SliceTiled(params InteractiveSprite9SliceTiledParams) Element {
	return internal.InteractiveImage9SliceTiled(params)
}

// Text returns the text visual and its params, for Element.Visual.
func Text(params TextParams) (ParamVisual[TextParams], TextParams) {
	return internal.Text(params)
}

// Label returns an element drawing text.
func Label(params TextParams) Element { return internal.Label(params) }

// InteractiveText returns the interactive text visual and its payload, for
// Element.Visual.
func InteractiveText(params InteractiveTextParams) (ParamVisual[interactiveTextPayload], interactiveTextPayload) {
	return internal.InteractiveText(params)
}

// InteractiveLabel returns an element drawing interactive text.
func InteractiveLabel(params InteractiveTextParams) Element {
	return internal.InteractiveLabel(params)
}

// Color returns the colour-fill visual and its params, for Element.Visual.
func Color(params ColorParams) (ParamVisual[ColorParams], ColorParams) {
	return internal.Color(params)
}

// ColorPanel returns an element filled with a solid colour.
func ColorPanel(params ColorParams) Element { return internal.ColorPanel(params) }

// InteractiveColor returns the interactive colour-fill visual and its payload,
// for Element.Visual.
func InteractiveColor(params InteractiveColorParams) (ParamVisual[interactiveColorPayload], interactiveColorPayload) {
	return internal.InteractiveColor(params)
}

// InteractiveColorPanel returns an element filled with a colour that varies by
// visual state.
func InteractiveColorPanel(params InteractiveColorParams) Element {
	return internal.InteractiveColorPanel(params)
}
