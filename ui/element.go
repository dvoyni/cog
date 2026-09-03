package ui

import (
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

type ID string

type opt[T any] struct {
	v   T
	set bool
}

type size struct {
	value    float32
	relative bool
}

type Layout uint8

const (
	LayoutNone Layout = iota
	LayoutHorizontal
	LayoutVertical
	LayoutGrid
)

type Alignment uint8

const (
	AlignStart Alignment = iota
	AlignCenter
	AlignEnd
	AlignStretch

	AlignTop    = AlignStart
	AlignMiddle = AlignCenter
	AlignBottom = AlignEnd

	AlignLeft  = AlignStart
	AlignRight = AlignEnd
)

type Arrangement uint8

const (
	ArrangeStart Arrangement = iota
	ArrangeCenter
	ArrangeEnd
	ArrangeSpaceBetween
	ArrangeSpaceAround

	ArrangeTop    = ArrangeStart
	ArrangeMiddle = ArrangeCenter
	ArrangeBottom = ArrangeEnd

	ArrangeLeft  = ArrangeStart
	ArrangeRight = ArrangeEnd
)

type Rect = m.Rect

type VisualState uint16

const (
	VisualDisabled VisualState = 1 << iota
	VisualActive
	VisualHovered
	VisualPressed
)

const VisualUserDefinedBase VisualState = 1 << 4

// visualInteractionStates is every state layout derives for itself, from the
// pointer or from a control being switched off, as opposed to the states an
// application defines for itself above VisualUserDefinedBase.
const visualInteractionStates = VisualDisabled | VisualActive | VisualHovered | VisualPressed

func (state VisualState) Has(mask VisualState) bool {
	return state&mask == mask
}

// Visual is what an Element stores: its params are already bound, so the layout
// pass measures and draws without knowing their type.
type Visual interface {
	DefaultSize(lookup canvas.LookupAccess) m.Vec2
	Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State)
}

// ParamVisual produces output from typed params. Implementations are stateless
// and shared between elements; Element.Visual binds one to the params of a single
// element.
type ParamVisual[T any] interface {
	DefaultSize(lookup canvas.LookupAccess, params T) m.Vec2
	Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params T)
}

// boundVisual pairs a stateless ParamVisual with one element's params. It is the
// only place the params are type-erased, and it erases them without asserting.
type boundVisual[T any] struct {
	visual ParamVisual[T]
	params T
}

func (b boundVisual[T]) DefaultSize(lookup canvas.LookupAccess) m.Vec2 {
	return b.visual.DefaultSize(lookup, b.params)
}

func (b boundVisual[T]) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State) {
	b.visual.Draw(lookup, queue, state, b.params)
}

type Element struct {
	id                                           ID
	userData                                     any
	width, minWidth, maxWidth                    opt[size]
	height, minHeight, maxHeight                 opt[size]
	left, right, top, bottom                     opt[size]
	pivotLeft, pivotRight, pivotTop, pivotBottom opt[size]
	paddingLeft, paddingRight                    opt[size]
	paddingTop, paddingBottom                    opt[size]
	stretch, shrink                              opt[float32]
	align                                        opt[Alignment]
	layer                                        opt[int]
	ignoreLayout                                 bool
	ignoreClip                                   bool
	ignoreHitTest                                bool
	stayOnScreen                                 bool
	preserveAspectRatio                          bool
	addState, removeState                        VisualState

	children            []Element
	layout              Layout
	childrenArrangement opt[Arrangement]
	childrenAlignment   opt[Alignment]
	gap                 opt[size]
	wrap                bool
	columns, rows       opt[int]

	visual Visual

	intermediate intermediate
}

type State struct {
	VisualState
	Rect, ContentRect, ClipRect Rect
	Layer                       canvas.Layer
}

type intermediate struct {
	state          State
	measured       m.Vec2
	contentMinimum m.Vec2
	aspectRatio    float32
	layer          canvas.Layer
	active         bool
}
