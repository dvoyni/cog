package input

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
