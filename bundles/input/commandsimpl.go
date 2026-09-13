package input

import "github.com/dvoyni/cog/kernel"

// applyCmdImpl folds a batch of input changes into the State (under its write
// lock) and publishes the discrete event for each change.
func applyCmdImpl() (kernel.Lock, kernel.Execute[ApplyRequest, ApplyResponse]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, request ApplyRequest) (ApplyResponse, error) {
			s := state.Get()
			for _, c := range request.Changes {
				s.apply(c)
				publish(k, c)
			}
			return ApplyResponse{}, nil
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
		}, func(k kernel.Kernel, request SynthesizeRequest) (StateResponse, error) {
			s := state.Get()
			for _, action := range request.Actions {
				synthesize(k, s, action)
			}
			return s.snapshot(), nil
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
	s.apply(change)
	if change.kind == changeKey {
		change.mods = s.modifiers()
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
		}, func(kernel.Kernel, StateRequest) (StateResponse, error) {
			return state.Get().snapshot(), nil
		}
}
