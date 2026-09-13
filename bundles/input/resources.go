package input

import "github.com/dvoyni/cog/bundles/input/internal"

// State is the polled input state, registered as the *State resource.
// Gameplay reads it under a read lock and queries it: Pressed, JustPressed,
// JustReleased, Pointer, Scroll and Text. The pressed set and the pointer are
// live; the edges, scroll and text are per-tick, promoted from what the input
// commands folded in by the AdvanceOnUpdate subscription. It is a concrete
// type, and folding changes into it is inputimpl's alone.
type State = internal.State
