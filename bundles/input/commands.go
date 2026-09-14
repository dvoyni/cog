package input

import (
	"github.com/dvoyni/cog/bundles/input/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// ApplyCmd applies a batch of input Changes to the State (under its write lock)
// and publishes the discrete input events.
type ApplyCmd kernel.Command[ApplyRequest, ApplyResponse]

// ApplyRequest is the request for ApplyCmd: the batch of input changes to fold in.
type ApplyRequest struct{ Changes []Change }

// ApplyResponse is the (empty) response from ApplyCmd.
type ApplyResponse struct{}

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
// It is declared in internal/types, where Play dispatches it, and aliased here.
type SynthesizeCmd = types.SynthesizeCmd

// SynthesizeRequest is one batch of a synthetic input sequence: the steps that
// land in the same tick.
type SynthesizeRequest = types.SynthesizeRequest

// StateCmd reports what the input seam holds, under the State read lock,
// changing nothing.
type StateCmd kernel.Command[StateRequest, StateResponse]

// StateRequest is the empty request for StateCmd.
type StateRequest struct{}

// StateResponse is the picture of the seam: what is held, and where the
// pointer is. SynthesizeCmd answers with it too, deliberately — every way in
// answers the same question, which is how a caller that leaked a key finds it.
//
// Down is every key and mouse button currently held, sorted by key code because
// map iteration is not ordered; a key nothing releases stays there. Pointer is
// where the pointer is, in window units.
type StateResponse = types.StateResponse
