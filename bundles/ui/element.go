package ui

import (
	"github.com/dvoyni/cog/bundles/ui/internal"
	"github.com/dvoyni/cog/libs/m"
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

	AlignTop    = internal.AlignTop
	AlignMiddle = internal.AlignMiddle
	AlignBottom = internal.AlignBottom

	AlignLeft  = internal.AlignLeft
	AlignRight = internal.AlignRight
)

// Arrangement distributes a container's children along its main axis.
type Arrangement = internal.Arrangement

const (
	ArrangeStart        = internal.ArrangeStart
	ArrangeCenter       = internal.ArrangeCenter
	ArrangeEnd          = internal.ArrangeEnd
	ArrangeSpaceBetween = internal.ArrangeSpaceBetween
	ArrangeSpaceAround  = internal.ArrangeSpaceAround

	ArrangeTop    = internal.ArrangeTop
	ArrangeMiddle = internal.ArrangeMiddle
	ArrangeBottom = internal.ArrangeBottom

	ArrangeLeft  = internal.ArrangeLeft
	ArrangeRight = internal.ArrangeRight
)

// Rect is a rectangle in logical viewport units.
type Rect = m.Rect

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

// NewElement returns an empty element declaration.
func NewElement() Element { return internal.NewElement() }
