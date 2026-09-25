package internal

import (
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
)

type ID string

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
	// AlignBaseline sits a row's children on one baseline: the first line of
	// text in each, whatever its font and size. It means something only across
	// a Horizontal; a child with no baseline, or under any other layout, is
	// placed as AlignStart.
	AlignBaseline

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

// BaselineVisual is a Visual that sits its content on a baseline, which
// AlignBaseline lines up across a row. A Visual without it has no baseline.
type BaselineVisual interface {
	// Baseline is how far below the top of a rect height tall the visual draws
	// its first baseline, and false when it draws none.
	Baseline(lookup canvas.LookupAccess, height float32) (float32, bool)
}

// ParamBaselineVisual is the ParamVisual side of BaselineVisual: a ParamVisual
// that implements it gives every element it is bound to a baseline.
type ParamBaselineVisual[T any] interface {
	Baseline(lookup canvas.LookupAccess, params T, height float32) (float32, bool)
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

// Baseline is the visual's baseline, when its ParamVisual reports one.
func (b boundVisual[T]) Baseline(lookup canvas.LookupAccess, height float32) (float32, bool) {
	visual, ok := b.visual.(ParamBaselineVisual[T])
	if !ok {
		return 0, false
	}
	return visual.Baseline(lookup, b.params, height)
}

func (b boundVisual[T]) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State) {
	b.visual.Draw(lookup, queue, state, b.params)
}

type Element struct {
	id                                           ID
	userData                                     any
	width, minWidth, maxWidth                    m.Maybe[size]
	height, minHeight, maxHeight                 m.Maybe[size]
	left, right, top, bottom                     m.Maybe[size]
	pivotLeft, pivotRight, pivotTop, pivotBottom m.Maybe[size]
	paddingLeft, paddingRight                    m.Maybe[size]
	paddingTop, paddingBottom                    m.Maybe[size]
	stretch, shrink                              m.Maybe[float32]
	align                                        m.Maybe[Alignment]
	layer                                        m.Maybe[int]
	material                                     m.Maybe[canvas.MaterialSet]
	ignoreLayout                                 bool
	ignoreClip                                   bool
	ignoreHitTest                                bool
	stayOnScreen                                 bool
	preserveAspectRatio                          bool
	addState, removeState                        VisualState

	children            []Element
	layout              Layout
	childrenArrangement m.Maybe[Arrangement]
	childrenAlignment   m.Maybe[Alignment]
	gap                 m.Maybe[size]
	wrap                bool
	columns, rows       m.Maybe[int]

	visual Visual

	intermediate intermediate
}

type State struct {
	VisualState
	Rect, ContentRect, ClipRect Rect
	Layer                       canvas.Layer
	// Materials is the material set this element inherited: the frame's default,
	// overridden by the nearest ancestor that named one, overridden by this
	// element's own. A Visual picks the slot for the family it draws - the sprite
	// slot for a sprite, a nine-slice, a glyph run or a fill, all of which are
	// sprite draws - and passes it as the draw's material, which is what makes
	// the whole mechanism record time and leaves the batch key untouched.
	//
	// A set's Params are here for a Visual that wants them; the built-in visuals
	// pass the slot alone. A parameter named at a sprite draw is per sprite and
	// becomes a storage array, so a per-scope value belongs on the material or on
	// canvas.OpQueue.SetLayerMaterial, where it is per batch by construction.
	Materials canvas.MaterialSet
}

type intermediate struct {
	state          State
	measured       m.Vec2
	contentMinimum m.Vec2
	aspectRatio    float32
	// baseline is how far below the top of its measured box the element's
	// first baseline sits, when it has one.
	baseline m.Maybe[float32]
	layer    canvas.Layer
	active   bool
}
