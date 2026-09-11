package input

import "slices"

// state is the polled input state. It is registered as *state; gameplay reads
// it under a read lock and queries it. The "just pressed/released", scroll, and
// text values are per-tick: a driver's Apply folds changes into pending
// accumulators, and the tick-boundary subscription promotes them to the current
// tick's view via advance. The pressed set and pointer are live (always current).
type state struct {
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

func newState() *state {
	return &state{
		down:         map[Key]struct{}{},
		justPressed:  map[Key]struct{}{},
		justReleased: map[Key]struct{}{},
		pendPressed:  map[Key]struct{}{},
		pendReleased: map[Key]struct{}{},
	}
}

// apply folds one change into the live state and the pending per-tick accumulators.
func (s *state) apply(c Change) {
	switch c.kind {
	case changeKey:
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
	case changePointer:
		s.pointer = c.pos
	case changeScroll:
		s.pendScrollDx += c.dx
		s.pendScrollDy += c.dy
	case changeText:
		s.pendText = append(s.pendText, c.r)
	}
}

// advance promotes the pending accumulators to the current-tick view and resets
// them. Called once per tick, before gameplay reads the state.
func (s *state) advance() {
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
func (s *state) Pressed(k Key) bool { _, ok := s.down[k]; return ok }

// JustPressed reports whether k transitioned to down during the current tick.
func (s *state) JustPressed(k Key) bool { _, ok := s.justPressed[k]; return ok }

// JustReleased reports whether k transitioned to up during the current tick.
func (s *state) JustReleased(k Key) bool { _, ok := s.justReleased[k]; return ok }

// Pointer returns the current pointer position (live).
func (s *state) Pointer() Pos { return s.pointer }

// Scroll returns the scroll delta accumulated for the current tick.
func (s *state) Scroll() (dx, dy float64) { return s.scrollDx, s.scrollDy }

// Text returns the runes typed during the current tick.
func (s *state) Text() []rune { return s.text }

// snapshot is the picture of the seam every input capability answers with: the
// live down-set, sorted because map iteration is not, and the live pointer. The
// slice is always non-nil, so "nothing is held" reads as an empty list rather
// than as a missing answer.
func (s *state) snapshot() StateResponse {
	down := make([]Key, 0, len(s.down))
	for key := range s.down {
		down = append(down, key)
	}
	slices.Sort(down)
	return StateResponse{Down: down, Pointer: s.pointer}
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
func (s *state) modifiers() Mods {
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
