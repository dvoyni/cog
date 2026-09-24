package ui

import "github.com/dvoyni/cog/bundles/ui/internal"

// ArmLayoutCmd arms one layout snapshot and hands back the wait. It is
// ordinary ui API: anything holding a kernel handle may ask what one tick's
// layout resolved to, and the agent-facing capability is one caller among
// them.
//
// The response's channel is the only delivery path, and refusals travel it
// too, because LayoutSnapshot carries Err - the same shape the other two
// snapshots' arms use, for the same reason: a channel passed in with the
// request would leave ui unable to refuse a second arm synchronously.
type ArmLayoutCmd = internal.ArmLayoutCmd

// ArmLayoutRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known: the serialization runs in the tick, and by then it already
// knows what was asked for.
//
// A subtree root and a depth cap are the two axes a tree wants; a flat
// sequence's page size and fetch-by-index are the wrong filters for one, which
// is why they are not here.
type ArmLayoutRequest = internal.ArmLayoutRequest

// ArmLayoutResponse hands back the wait and the viewport.
type ArmLayoutResponse = internal.ArmLayoutResponse
