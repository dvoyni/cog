package platform

// InputState is the resource of the input levels as of the last
// ProcessEvents run, as a *InputState: the keys held, which are global, and
// the keys pressed and released during that run. It is written only by
// ProcessEvents; any job reads it under the read lock.
//
// The edges, JustPressed and JustReleased, describe the last run, not the
// last time a reader looked. They are correct for the jobs a handler
// schedules after ProcessEvents in the same publication, the update that
// processed the events. Every other reader sees them wrong: a job of another
// track, or of a later beat that ran no pump, sees the last run's edges
// again, and the steps of a fixed-step catch-up all see the same ones. Such a
// reader folds KeyEvents in its own handler instead.
type InputState struct {
	held     keySet
	pressed  keySet
	released keySet
}

// IsHeld answers whether key is held down.
func (s *InputState) IsHeld(key Key) bool { return s.held.has(key) }

// JustPressed answers whether key went down during the last ProcessEvents
// run; a repeat is no press. A key pressed and released in that one run
// answers true here and in JustReleased, and is not held. It is correct only
// in the update that ran the pump: see InputState.
func (s *InputState) JustPressed(key Key) bool { return s.pressed.has(key) }

// JustReleased answers whether key went up during the last ProcessEvents
// run, a release on focus loss included. It is correct only in the update
// that ran the pump: see InputState.
func (s *InputState) JustReleased(key Key) bool { return s.released.has(key) }

// clearEdges forgets the edges of the run before, as a run starts.
func (s *InputState) clearEdges() {
	s.pressed = keySet{}
	s.released = keySet{}
}

// press marks key held, and just pressed if it was not held.
func (s *InputState) press(key Key) {
	if !s.held.has(key) {
		s.pressed.add(key)
	}
	s.held.add(key)
}

// release marks key not held, and just released.
func (s *InputState) release(key Key) {
	s.held.remove(key)
	s.released.add(key)
}

// keySet is a set of Keys, a bit each.
type keySet [(keyCount + 63) / 64]uint64

// has answers whether key is in the set; a value past the last Key never is.
func (set *keySet) has(key Key) bool {
	return key.isValid() && set[key/64]&(1<<(key%64)) != 0
}

// add puts key in the set.
func (set *keySet) add(key Key) { set[key/64] |= 1 << (key % 64) }

// remove takes key out of the set.
func (set *keySet) remove(key Key) { set[key/64] &^= 1 << (key % 64) }
