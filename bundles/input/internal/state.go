package internal

import "slices"

// State is the polled input state. It is registered as *State; gameplay reads
// it under a read lock and queries it. The "just pressed/released", scroll, and
// text values are per-tick: a driver's Apply folds changes into pending
// accumulators, and the tick-boundary subscription promotes them to the current
// tick's view via advance. The pressed set and pointer are live (always current).
type State struct {
	down map[Key]struct{}

	// current tick view (read by gameplay)
	justPressed  map[Key]struct{}
	justReleased map[Key]struct{}
	scrollDx     float64
	scrollDy     float64
	text         []rune

	// pending accumulation since the last tick (written by Apply)
	pendPressed  map[Key]struct{}
	pendReleased map[Key]struct{}
	pendScrollDx float64
	pendScrollDy float64
	pendText     []rune

	pointer Pos
}

func NewState() *State {
	return &State{
		down:         map[Key]struct{}{},
		justPressed:  map[Key]struct{}{},
		justReleased: map[Key]struct{}{},
		pendPressed:  map[Key]struct{}{},
		pendReleased: map[Key]struct{}{},
	}
}

// apply folds one change into the live state and the pending per-tick accumulators.
func (s *State) apply(c Change) {
	switch c.kind {
	case ChangeKindKey:
		if c.down {
			if _, ok := s.down[c.key]; !ok {
				s.down[c.key] = struct{}{}
				s.pendPressed[c.key] = struct{}{}
			}
		} else {
			if _, ok := s.down[c.key]; ok {
				delete(s.down, c.key)
				s.pendReleased[c.key] = struct{}{}
			}
		}
	case ChangeKindPointer:
		s.pointer = c.pos
	case ChangeKindScroll:
		s.pendScrollDx += c.dx
		s.pendScrollDy += c.dy
	case ChangeKindText:
		s.pendText = append(s.pendText, c.r)
	}
}

// advance promotes the pending accumulators to the current-tick view and resets
// them. Called once per tick, before gameplay reads the state.
func (s *State) advance() {
	s.justPressed = s.pendPressed
	s.justReleased = s.pendReleased
	s.scrollDx, s.scrollDy = s.pendScrollDx, s.pendScrollDy
	s.text = s.pendText

	s.pendPressed = map[Key]struct{}{}
	s.pendReleased = map[Key]struct{}{}
	s.pendScrollDx, s.pendScrollDy = 0, 0
	s.pendText = nil
}

// Pressed reports whether k is currently held down.
func (s *State) Pressed(k Key) bool { _, ok := s.down[k]; return ok }

// JustPressed reports whether k transitioned to down during the current tick.
func (s *State) JustPressed(k Key) bool { _, ok := s.justPressed[k]; return ok }

// JustReleased reports whether k transitioned to up during the current tick.
func (s *State) JustReleased(k Key) bool { _, ok := s.justReleased[k]; return ok }

// Pointer returns the current pointer position (live).
func (s *State) Pointer() Pos { return s.pointer }

// Scroll returns the scroll delta accumulated for the current tick.
func (s *State) Scroll() (dx, dy float64) { return s.scrollDx, s.scrollDy }

// Text returns the runes typed during the current tick.
func (s *State) Text() []rune { return s.text }

// held is the down-set of the picture of the seam every input capability
// answers with (input.StateResponse), which pairs it with the live pointer:
// the live down-set, sorted because map iteration is not. The slice is always
// non-nil, so "nothing is held" reads as an empty list rather than as a missing
// answer.
func (s *State) held() []Key {
	down := make([]Key, 0, len(s.down))
	for key := range s.down {
		down = append(down, key)
	}
	slices.Sort(down)
	return down
}

// modifiers derives the modifier bitmask from the live down-set. A driver
// carries Mods from the platform and the state does not track them, so this is
// the only place a chord assembled from separate presses can report the
// modifier that is actually held instead of zero.
//
// ModCapsLock and ModNumLock are never derived: they report a lock being
// active, not a key being held, and nothing in the state knows which locks are
// on. Reporting them from a held key would be a lie in the one direction that
// matters — a caller comparing Pressed against Mods.
func (s *State) modifiers() Mods {
	var mods Mods
	for key, mod := range modifierKeys {
		if _, held := s.down[key]; held {
			mods |= mod
		}
	}
	return mods
}

// modifierKeys maps each modifier key onto the bit it contributes. Left and
// right contribute the same bit, as they do on every platform cog maps from.
var modifierKeys = map[Key]Mods{
	KeyLeftShift:    ModShift,
	KeyRightShift:   ModShift,
	KeyLeftControl:  ModCtrl,
	KeyRightControl: ModCtrl,
	KeyLeftAlt:      ModAlt,
	KeyRightAlt:     ModAlt,
	KeyLeftSuper:    ModSuper,
	KeyRightSuper:   ModSuper,
}
