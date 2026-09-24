package internal

import (
	"github.com/dvoyni/cog/kernel"
)

// applyCmdImpl folds a batch of input changes into the State (under its write
// lock) and publishes the discrete event for each change.
func applyCmdImpl() (kernel.Lock, kernel.Execute[ApplyRequest, ApplyResponse]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, request ApplyRequest) ApplyResponse {
			s := state.Get()
			for _, c := range request.Changes {
				StateApply(s, c)
				publish(k, c)
			}
			return ApplyResponse{}
		}
}

// synthesizeCmdImpl folds one batch of a synthetic sequence into the State
// under one write lock and publishes the same events applyCmdImpl does. One
// lock hold is the batch's only atomicity guarantee, and it is the one that
// matters: a human's mouse move cannot land between a move and the key press
// that follows it.
func synthesizeCmdImpl() (kernel.Lock, kernel.Execute[SynthesizeRequest, StateResponse]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, request SynthesizeRequest) StateResponse {
			s := state.Get()
			for _, action := range request.Actions {
				synthesize(k, s, action)
			}
			return snapshot(s)
		}
}

// synthesize applies one step. Text is the only step that is more than one
// change, and delay is the only step that is none: it splits a sequence rather
// than applying anything, and the split belongs to Play, outside every lock.
func synthesize(k kernel.Kernel, s *State, action Action) {
	if action.Do == ActionText {
		for _, r := range action.Text {
			fold(k, s, TextChange(r))
		}
		return
	}
	change, applicable := changeFor(s, action)
	if !applicable {
		return
	}
	fold(k, s, change)
}

// fold applies one change and publishes its event. Mods are read from the
// down-set after the change is folded, so pressing Shift reports ModShift and a
// Ctrl+S assembled from two steps reports ModCtrl on the S — and the held state
// and the reported modifiers can never disagree. Upstream platforms are
// inconsistent about a modifier's own press event; cog takes the self-consistent
// reading.
func fold(k kernel.Kernel, s *State, change Change) {
	StateApply(s, change)
	if ChangeKindOf(&change) == ChangeKindKey {
		*ChangeModsRef(&change) = StateModifiers(s)
	}
	publish(k, change)
}

// changeFor turns one step into the change a driver would have built, and
// reports false for a step that builds none. move_by resolves against the
// pointer under the lock, which is the one thing the caller's own arithmetic
// over a returned position cannot be.
func changeFor(s *State, action Action) (Change, bool) {
	switch action.Do {
	case ActionKeyDown:
		return KeyChange(action.Key, 0, true), true
	case ActionKeyUp:
		return KeyChange(action.Key, 0, false), true
	case ActionMove:
		return PointerChange(Pos{X: action.X, Y: action.Y}), true
	case ActionMoveBy:
		at := s.Pointer()
		return PointerChange(Pos{X: at.X + action.Dx, Y: at.Y + action.Dy}), true
	case ActionScroll:
		return ScrollChange(action.Dx, action.Dy), true
	default:
		return Change{}, false
	}
}

// stateCmdImpl reads the seam under the State read lock and changes nothing.
func stateCmdImpl() (kernel.Lock, kernel.Execute[StateRequest, StateResponse]) {
	var state kernel.Read[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetRead[*State]()
		}, func(kernel.Kernel, StateRequest) StateResponse {
			return snapshot(state.Get())
		}
}

// snapshot is the picture of the seam every input capability answers with: the
// live down-set, sorted because map iteration is not, and the live pointer. The
// slice is always non-nil, so "nothing is held" reads as an empty list rather
// than as a missing answer.
func snapshot(s *State) StateResponse {
	return StateResponse{Down: StateHeld(s), Pointer: s.Pointer()}
}

// SynthesizeCmd folds one batch of a synthetic input sequence into the State
// (under its write lock), publishes the same discrete events a driver's batch
// would, and reports the resulting seam.
//
// Play dispatches it from this package. The root aliases it with its request
// and response, and its documentation is there.
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

// ApplyCmd applies a batch of input Changes to the State (under its write lock)
// and publishes the discrete input events.
type ApplyCmd kernel.Command[ApplyRequest, ApplyResponse]

// ApplyRequest is the request for ApplyCmd: the batch of input changes to fold in.
type ApplyRequest struct{ Changes []Change }

// ApplyResponse is the (empty) response from ApplyCmd.
type ApplyResponse struct{}

// StateCmd reports what the input seam holds, under the State read lock,
// changing nothing.
type StateCmd kernel.Command[StateRequest, StateResponse]

// StateRequest is the empty request for StateCmd.
type StateRequest struct{}
