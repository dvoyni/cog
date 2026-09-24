package input

import (
	"github.com/dvoyni/cog/bundles/input/internal"
)

// ApplyCmd applies a batch of input Changes to the State (under its write lock)
// and publishes the discrete input events.
type ApplyCmd = internal.ApplyCmd

// ApplyRequest is the request for ApplyCmd: the batch of input changes to fold in.
type ApplyRequest = internal.ApplyRequest

// ApplyResponse is the (empty) response from ApplyCmd.
type ApplyResponse = internal.ApplyResponse

// SynthesizeCmd folds one batch of a synthetic input sequence into the State
// (under its write lock), publishes the same discrete events a driver's batch
// would, and reports the resulting seam.
//
// A plain function could build []Change and dispatch ApplyCmd instead; it is
// pure computation over no state. A handler holding the write lock does three
// things that route cannot: it derives Mods from the live down-set after the
// change is folded, so a synthetic chord reports the modifier that is held
// rather than zero; it resolves ActionMoveBy, which needs the current pointer;
// and it reads the resulting state back out of the lock it already holds.
//
// It never waits. ActionDelay is ignored here and honoured by Play, because a
// handler that slept would sleep under the State write lock and stall every
// tick for the delay's duration.
//
// It is declared in internal, where Play dispatches it, and aliased here.
type SynthesizeCmd = internal.SynthesizeCmd

// SynthesizeRequest is one batch of a synthetic input sequence: the steps that
// land in the same tick.
type SynthesizeRequest = internal.SynthesizeRequest

// StateCmd reports what the input seam holds, under the State read lock,
// changing nothing.
type StateCmd = internal.StateCmd

// StateRequest is the empty request for StateCmd.
type StateRequest = internal.StateRequest

// StateResponse is the picture of the seam: what is held, and where the
// pointer is. SynthesizeCmd answers with it too, deliberately — every way in
// answers the same question, which is how a caller that leaked a key finds it.
//
// Down is every key and mouse button currently held, sorted by key code because
// map iteration is not ordered; a key nothing releases stays there. Pointer is
// where the pointer is, in window units.
type StateResponse = internal.StateResponse
