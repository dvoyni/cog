package internal

import (
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// applyCmdImpl folds a batch of input changes into the State (under its write
// lock) and publishes the discrete event for each change.
func applyCmdImpl() (kernel.Lock, kernel.Execute[input.ApplyRequest, input.ApplyResponse]) {
	var state kernel.Write[*input.State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*input.State]()
		}, func(k kernel.Kernel, request input.ApplyRequest) (input.ApplyResponse, error) {
			s := state.Get()
			for _, c := range request.Changes {
				types.StateApply(s, c)
				publish(k, c)
			}
			return input.ApplyResponse{}, nil
		}
}

// synthesizeCmdImpl folds one batch of a synthetic sequence into the State
// under one write lock and publishes the same events applyCmdImpl does. One
// lock hold is the batch's only atomicity guarantee, and it is the one that
// matters: a human's mouse move cannot land between a move and the key press
// that follows it.
func synthesizeCmdImpl() (kernel.Lock, kernel.Execute[input.SynthesizeRequest, input.StateResponse]) {
	var state kernel.Write[*input.State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*input.State]()
		}, func(k kernel.Kernel, request input.SynthesizeRequest) (input.StateResponse, error) {
			s := state.Get()
			for _, action := range request.Actions {
				synthesize(k, s, action)
			}
			return snapshot(s), nil
		}
}

// synthesize applies one step. Text is the only step that is more than one
// change, and delay is the only step that is none: it splits a sequence rather
// than applying anything, and the split belongs to Play, outside every lock.
func synthesize(k kernel.Kernel, s *input.State, action input.Action) {
	if action.Do == input.ActionText {
		for _, r := range action.Text {
			fold(k, s, input.TextChange(r))
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
func fold(k kernel.Kernel, s *input.State, change input.Change) {
	types.StateApply(s, change)
	if types.ChangeKindOf(&change) == types.ChangeKindKey {
		*types.ChangeModsRef(&change) = types.StateModifiers(s)
	}
	publish(k, change)
}

// changeFor turns one step into the change a driver would have built, and
// reports false for a step that builds none. move_by resolves against the
// pointer under the lock, which is the one thing the caller's own arithmetic
// over a returned position cannot be.
func changeFor(s *input.State, action input.Action) (input.Change, bool) {
	switch action.Do {
	case input.ActionKeyDown:
		return input.KeyChange(action.Key, 0, true), true
	case input.ActionKeyUp:
		return input.KeyChange(action.Key, 0, false), true
	case input.ActionMove:
		return input.PointerChange(input.Pos{X: action.X, Y: action.Y}), true
	case input.ActionMoveBy:
		at := s.Pointer()
		return input.PointerChange(input.Pos{X: at.X + action.Dx, Y: at.Y + action.Dy}), true
	case input.ActionScroll:
		return input.ScrollChange(action.Dx, action.Dy), true
	default:
		return input.Change{}, false
	}
}

// stateCmdImpl reads the seam under the State read lock and changes nothing.
func stateCmdImpl() (kernel.Lock, kernel.Execute[input.StateRequest, input.StateResponse]) {
	var state kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			state = access.GetRead[*input.State]()
		}, func(kernel.Kernel, input.StateRequest) (input.StateResponse, error) {
			return snapshot(state.Get()), nil
		}
}

// snapshot is the picture of the seam every input capability answers with: the
// live down-set, sorted because map iteration is not, and the live pointer. The
// slice is always non-nil, so "nothing is held" reads as an empty list rather
// than as a missing answer.
func snapshot(s *input.State) input.StateResponse {
	return input.StateResponse{Down: types.StateHeld(s), Pointer: s.Pointer()}
}
