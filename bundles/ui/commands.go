package ui

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// ArmLayoutCmd arms one layout snapshot and hands back the wait. It is
// ordinary ui API: anything holding a kernel handle may ask what one tick's
// layout resolved to, and the agent-facing capability is one caller among
// them.
//
// The response's channel is the only delivery path, and refusals travel it
// too, because LayoutSnapshot carries Err - the same shape the other two
// snapshots' arms use, for the same reason: a channel passed in with the
// request would leave ui unable to refuse a second arm synchronously.
type ArmLayoutCmd kernel.Command[ArmLayoutRequest, ArmLayoutResponse]

// ArmLayoutRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known: the serialization runs in the tick, and by then it already
// knows what was asked for.
//
// A subtree root and a depth cap are the two axes a tree wants; a flat
// sequence's page size and fetch-by-index are the wrong filters for one, which
// is why they are not here.
type ArmLayoutRequest struct {
	// Subtree is the source index of the element the report starts at. Flatten
	// is depth-first pre-order, so a subtree is a contiguous index range and
	// the filter is a slice rather than a traversal.
	Subtree m.Maybe[int]
	// MaxDepth keeps elements no deeper than this below the reported root -
	// zero is the root alone, one is the root and its children. Depth is
	// measured from the subtree root when there is one and from each tree root
	// otherwise.
	MaxDepth m.Maybe[int]
}

// ArmLayoutResponse hands back the wait and the viewport.
type ArmLayoutResponse struct {
	// Done receives exactly one LayoutSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan LayoutSnapshot
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. A resize between the arm and the tick it binds to is a stated
	// non-guarantee, exactly as it is for a capture.
	Viewport gfx.Viewport
}
