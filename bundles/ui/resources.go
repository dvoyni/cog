package ui

import (
	"github.com/dvoyni/cog/bundles/ui/internal"
	"github.com/dvoyni/cog/libs/m"
)

// Frame collects the complete UI declaration for one update tick. It is a
// writable resource: a producer adds its roots before ProcessOnUpdate, which
// consumes and clears it. Consuming it is uiimpl's alone.
type Frame = internal.Frame

// Interactions contains the results from the most recently processed UI frame.
// A reader before ProcessOnUpdate sees the previous tick's, and a reader after
// it this tick's.
type Interactions = internal.Interactions

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

// Measure resolves element against the available area and returns its arranged
// size without drawing or processing interactions. It has no canvas lookup, so
// intrinsic sprite and text sizes contribute zero.
func Measure(element Element, available m.Vec2) m.Vec2 {
	return internal.Measure(element, available)
}
