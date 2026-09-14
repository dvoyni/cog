package ui

import (
	"github.com/dvoyni/cog/bundles/ui/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// ID names an element for hit testing. IDs are hierarchical strings, and
// Interactions.Has matches them by prefix.
type ID = types.ID

// Layout is how an element arranges its children.
type Layout = types.Layout

const (
	LayoutNone       = types.LayoutNone
	LayoutHorizontal = types.LayoutHorizontal
	LayoutVertical   = types.LayoutVertical
	LayoutGrid       = types.LayoutGrid
)

// Alignment places an element across its parent's main axis.
type Alignment = types.Alignment

const (
	AlignStart   = types.AlignStart
	AlignCenter  = types.AlignCenter
	AlignEnd     = types.AlignEnd
	AlignStretch = types.AlignStretch

	AlignTop    = types.AlignTop
	AlignMiddle = types.AlignMiddle
	AlignBottom = types.AlignBottom

	AlignLeft  = types.AlignLeft
	AlignRight = types.AlignRight
)

// Arrangement distributes a container's children along its main axis.
type Arrangement = types.Arrangement

const (
	ArrangeStart        = types.ArrangeStart
	ArrangeCenter       = types.ArrangeCenter
	ArrangeEnd          = types.ArrangeEnd
	ArrangeSpaceBetween = types.ArrangeSpaceBetween
	ArrangeSpaceAround  = types.ArrangeSpaceAround

	ArrangeTop    = types.ArrangeTop
	ArrangeMiddle = types.ArrangeMiddle
	ArrangeBottom = types.ArrangeBottom

	ArrangeLeft  = types.ArrangeLeft
	ArrangeRight = types.ArrangeRight
)

// Rect is a rectangle in logical viewport units.
type Rect = m.Rect

// VisualState is the mask of presentation states an element resolved to,
// inherited through the tree. Bits below VisualUserDefinedBase are ui's own.
type VisualState = types.VisualState

const (
	VisualDisabled = types.VisualDisabled
	VisualActive   = types.VisualActive
	VisualHovered  = types.VisualHovered
	VisualPressed  = types.VisualPressed
)

// VisualUserDefinedBase is the first bit an application may define a
// presentation state of its own on.
const VisualUserDefinedBase = types.VisualUserDefinedBase

// Visual is what an Element stores: its params are already bound, so the layout
// pass measures and draws without knowing their type.
type Visual = types.Visual

// ParamVisual produces output from typed params. Implementations are stateless
// and shared between elements; Element.Visual binds one to the params of a single
// element.
type ParamVisual[T any] = types.ParamVisual[T]

// Element is a frame-local value describing layout, interaction and visual
// intent. Its Modifiers return a changed copy, so declarations compose
// fluently. The zero value is a valid empty declaration.
type Element = types.Element

// State is what a Visual draws with: the element's resolved visual state,
// rects, clip, layer and inherited material set.
type State = types.State

// ButtonParams names a Button and says whether it is disabled.
type ButtonParams = types.ButtonParams

// SpriteFit is how a sprite fills an element whose rect does not match the
// sprite's own ratio.
type SpriteFit = types.SpriteFit

const (
	SpriteStretch = types.SpriteStretch
	SpriteContain = types.SpriteContain
	SpriteCover   = types.SpriteCover
)

// SpriteParams describes one sprite visual: its path, scale, tint, key colour,
// filter, fit, source frame and rotation.
type SpriteParams = types.SpriteParams

// VisualStates maps presentation states to the value an interactive visual
// draws with while an element is in them. A key with several bits assigns the
// value to each, and drawing takes the value of the highest active bit.
type VisualStates[T any] = types.VisualStates[T]

// InteractiveSpriteParams is a sprite whose path and tint vary by visual state.
type InteractiveSpriteParams = types.InteractiveSpriteParams

// Sprite9SlicedParams describes a nine-slice drawn from one framed texture.
type Sprite9SlicedParams = types.Sprite9SlicedParams

// InteractiveSprite9SlicedParams is a nine-slice whose path and tint vary by
// visual state.
type InteractiveSprite9SlicedParams = types.InteractiveSprite9SlicedParams

// Insets are the four edge widths of a tiled nine-slice.
type Insets = types.Insets

// Sprite9SliceImages names the separate images of a tiled nine-slice.
type Sprite9SliceImages = types.Sprite9SliceImages

// Sprite9SliceTiledParams describes a nine-slice drawn from separate corner,
// edge and centre images, the edges and centre tiled.
type Sprite9SliceTiledParams = types.Sprite9SliceTiledParams

// InteractiveSprite9SliceTiledParams is a tiled nine-slice whose images and
// tint vary by visual state.
type InteractiveSprite9SliceTiledParams = types.InteractiveSprite9SliceTiledParams

// TextAlignment aligns a label's text inside its rect.
type TextAlignment = types.TextAlignment

const (
	TextAlignStart  = types.TextAlignStart
	TextAlignCenter = types.TextAlignCenter
	TextAlignEnd    = types.TextAlignEnd
)

// Font identifies a font face for UI text: a resource path and a logical pixel
// size. Text is measured and drawn through the canvas Lookup, which parses inline
// ${path} icons in every UI string.
//
// An empty Path draws with the font canvas embeds (canvas.DefaultFontPath), so a
// Font that names only a Size renders rather than disappearing. Size has no
// default: a label with no size is still nothing.
type Font = types.Font

// TextParams describes one text visual.
type TextParams = types.TextParams

// InteractiveTextParams is text whose colour varies by visual state.
type InteractiveTextParams = types.InteractiveTextParams

// ColorParams describes a solid colour fill.
type ColorParams = types.ColorParams

// InteractiveColorParams is a colour fill that varies by visual state.
type InteractiveColorParams = types.InteractiveColorParams

// The payloads the interactive visuals bind: the params with their state maps
// packed for drawing. Nothing outside ui names them.
type (
	interactiveSpritePayload            = types.InteractiveSpritePayload
	interactiveSprite9SlicedPayload     = types.InteractiveSprite9SlicedPayload
	interactiveSprite9SliceTiledPayload = types.InteractiveSprite9SliceTiledPayload
	interactiveTextPayload              = types.InteractiveTextPayload
	interactiveColorPayload             = types.InteractiveColorPayload
)

// Interaction is one thing the pointer did to an element with an ID.
type Interaction = types.Interaction

// InteractionKind is what an Interaction was.
type InteractionKind = types.InteractionKind

const (
	InteractionNone  = types.InteractionNone
	InteractionClick = types.InteractionClick
	InteractionHover = types.InteractionHover
	InteractionDown  = types.InteractionDown
	InteractionUp    = types.InteractionUp
	InteractionIn    = types.InteractionIn
	InteractionOut   = types.InteractionOut
)

// HoverTracker remembers what the pointer is resting on and for how long, so
// callers can decide whether to show a tooltip without pairing up InteractionIn
// and InteractionOut themselves.
//
// Interactions describe the frame the plugin last processed, so the tracker
// trails the pointer by one frame. The zero value reports hover immediately.
type HoverTracker = types.HoverTracker

// LayoutSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type LayoutSnapshot struct {
	Layout LayoutView
	// Tick is app.UpdateEvent.Tick of the tick the snapshot was taken in. It
	// travels with the snapshot rather than being asked for afterwards,
	// because only the tick itself knows which one it was.
	Tick int64
	Err  error
}

// LayoutView is one tick's element tree with what layout resolved it to,
// rendered inside the tick while the tree is still alive. It is flat, in
// depth-first pre-order, and every index in it is a source index, stable under
// filtering.
type LayoutView = types.LayoutView

// ElementView is one element: where layout put it, and - where it asked for
// something - what it asked for, beside what it got.
type ElementView = types.ElementView

// DeclaredView is what an element declared, beside what the resolved fields
// say it got. Every field is absent unless it was set.
type DeclaredView = types.DeclaredView

// SizeView is one declared length: the number the app wrote, and whether it
// wrote it as a fraction of the parent.
type SizeView = types.SizeView

// RectView is a rectangle in viewport units, which is the space every ui rect
// is in.
type RectView = types.RectView
