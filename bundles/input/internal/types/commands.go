package types

import "github.com/dvoyni/cog/kernel"

// SynthesizeCmd folds one batch of a synthetic input sequence into the State
// (under its write lock), publishes the same discrete events a driver's batch
// would, and reports the resulting seam.
//
// It is declared here rather than in the root because Play dispatches it, and
// this package never imports its root. The root aliases it with its request and
// response, and its documentation is there.
type SynthesizeCmd kernel.Command[SynthesizeRequest, StateResponse]

// SynthesizeRequest is one batch of a synthetic input sequence: the steps that
// land in the same tick.
type SynthesizeRequest struct {
	Actions []Action `json:"actions" jsonschema:"the steps to apply, in order"`
}

// StateResponse is the picture of the seam: what is held, and where the
// pointer is. SynthesizeCmd answers with it too, deliberately — every way in
// answers the same question, which is how a caller that leaked a key finds it.
type StateResponse struct {
	// Down is every key and mouse button currently held, sorted by key code
	// because map iteration is not ordered. A key nothing releases stays here.
	Down []Key `json:"down" jsonschema:"every key and mouse button currently held"`
	// Pointer is where the pointer is, in window units.
	Pointer Pos `json:"pointer" jsonschema:"the pointer position in window units"`
}
