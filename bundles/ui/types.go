package ui

import (
	"github.com/dvoyni/cog/bundles/ui/internal"
)

// ID names an element for hit testing. IDs are hierarchical strings, and
// Interactions.Has matches them by prefix.
type ID = internal.ID

// Layout is how an element arranges its children.
type Layout = internal.Layout

const (
	LayoutNone       = internal.LayoutNone
	LayoutHorizontal = internal.LayoutHorizontal
	LayoutVertical   = internal.LayoutVertical
	LayoutGrid       = internal.LayoutGrid
)

// Alignment places an element across its parent's main axis.
type Alignment = internal.Alignment

const (
	AlignStart   = internal.AlignStart
	AlignCenter  = internal.AlignCenter
	AlignEnd     = internal.AlignEnd
	AlignStretch = internal.AlignStretch
	AlignTop     = internal.AlignTop
	AlignMiddle  = internal.AlignMiddle
	AlignBottom  = internal.AlignBottom
	AlignLeft    = internal.AlignLeft
	AlignRight   = internal.AlignRight
)

// Arrangement distributes a container's children along its main axis.
type Arrangement = internal.Arrangement

const (
	ArrangeStart        = internal.ArrangeStart
	ArrangeCenter       = internal.ArrangeCenter
	ArrangeEnd          = internal.ArrangeEnd
	ArrangeSpaceBetween = internal.ArrangeSpaceBetween
	ArrangeSpaceAround  = internal.ArrangeSpaceAround
	ArrangeTop          = internal.ArrangeTop
	ArrangeMiddle       = internal.ArrangeMiddle
	ArrangeBottom       = internal.ArrangeBottom
	ArrangeLeft         = internal.ArrangeLeft
	ArrangeRight        = internal.ArrangeRight
)

// Rect is a rectangle in logical viewport units.
type Rect = internal.Rect

// VisualState is the mask of presentation states an element resolved to,
// inherited through the tree. Bits below VisualUserDefinedBase are ui's own.
type VisualState = internal.VisualState

const (
	VisualDisabled = internal.VisualDisabled
	VisualActive   = internal.VisualActive
	VisualHovered  = internal.VisualHovered
	VisualPressed  = internal.VisualPressed
)

// VisualUserDefinedBase is the first bit an application may define a
// presentation state of its own on.
const VisualUserDefinedBase = internal.VisualUserDefinedBase

// Visual is what an Element stores: its params are already bound, so the layout
// pass measures and draws without knowing their type.
type Visual = internal.Visual

// ParamVisual produces output from typed params. Implementations are stateless
// and shared between elements; Element.Visual binds one to the params of a single
// element.
type ParamVisual[T any] = internal.ParamVisual[T]

// Element is a frame-local value describing layout, interaction and visual
// intent. Its Modifiers return a changed copy, so declarations compose
// fluently. The zero value is a valid empty declaration.
type Element = internal.Element

// State is what a Visual draws with: the element's resolved visual state,
// rects, clip, layer and inherited material set.
type State = internal.State

// ButtonParams names a Button and says whether it is disabled.
type ButtonParams = internal.ButtonParams

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

// Interaction is one thing the pointer did to an element with an ID.
type Interaction = internal.Interaction

// InteractionKind is what an Interaction was.
type InteractionKind = internal.InteractionKind

const (
	InteractionNone  = internal.InteractionNone
	InteractionClick = internal.InteractionClick
	InteractionHover = internal.InteractionHover
	InteractionDown  = internal.InteractionDown
	InteractionUp    = internal.InteractionUp
	InteractionIn    = internal.InteractionIn
	InteractionOut   = internal.InteractionOut
)

// HoverTracker remembers what the pointer is resting on and for how long, so
// callers can decide whether to show a tooltip without pairing up InteractionIn
// and InteractionOut themselves.
//
// Interactions describe the frame the plugin last processed, so the tracker
// trails the pointer by one frame. The zero value reports hover immediately.
type HoverTracker = internal.HoverTracker

// LayoutSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type LayoutSnapshot = internal.LayoutSnapshot

// LayoutView is one tick's element tree with what layout resolved it to,
// rendered inside the tick while the tree is still alive. It is flat, in
// depth-first pre-order, and every index in it is a source index, stable under
// filtering.
type LayoutView = internal.LayoutView

// ElementView is one element: where layout put it, and - where it asked for
// something - what it asked for, beside what it got.
type ElementView = internal.ElementView

// DeclaredView is what an element declared, beside what the resolved fields
// say it got. Every field is absent unless it was set.
type DeclaredView = internal.DeclaredView

// SizeView is one declared length: the number the app wrote, and whether it
// wrote it as a fraction of the parent.
type SizeView = internal.SizeView

// RectView is a rectangle in viewport units, which is the space every ui rect
// is in.
type RectView = internal.RectView
