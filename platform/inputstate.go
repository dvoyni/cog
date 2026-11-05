package platform

import (
	"fmt"
	"slices"
)

// InputState is the resource of the input levels as of the last
// ProcessEvents run, as a *InputState: the keys held, which are global, each
// window's pointer position and whether the pointer is inside it, and the
// keys pressed and released during that run. It is written only by
// ProcessEvents; any job reads it under the read lock. Its pointer getters
// given the zero Window read the only window the window state knows, and
// panic unless it knows exactly one.
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
	// pointers holds the pointer of every window the window state knows, in
	// the order they were opened.
	pointers []windowPointer
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

// PointerPosition answers where the pointer last was in window, in its
// window space: inside it, or outside while it captured a drag. A window the
// pointer never entered nor moved in answers the zero Point.
func (s *InputState) PointerPosition(window Window) Point { return s.pointer(window).position }

// IsPointerInside answers whether the pointer is over window.
func (s *InputState) IsPointerInside(window Window) bool { return s.pointer(window).inside }

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

// addPointer tracks the pointer of window, which the window state now
// knows.
func (s *InputState) addPointer(window Window) {
	s.pointers = append(s.pointers, windowPointer{window: window})
}

// enterPointer marks the pointer inside window, at position.
func (s *InputState) enterPointer(window Window, position Point) {
	if index := s.findPointer(window); index >= 0 {
		s.pointers[index].position = position
		s.pointers[index].placed = true
		s.pointers[index].inside = true
	}
}

// leavePointer marks the pointer gone from window, where it keeps its last
// position.
func (s *InputState) leavePointer(window Window) {
	if index := s.findPointer(window); index >= 0 {
		s.pointers[index].inside = false
	}
}

// movePointer moves window's pointer to position, and answers the motion
// from where it was: none for the first move in a window the pointer did not
// enter.
func (s *InputState) movePointer(window Window, position Point) Point {
	index := s.findPointer(window)
	if index < 0 {
		return Point{}
	}
	pointer := &s.pointers[index]
	var motion Point
	if pointer.placed {
		motion = Point{X: position.X - pointer.position.X, Y: position.Y - pointer.position.Y}
	}
	pointer.position = position
	pointer.placed = true
	return motion
}

// forgetPointer forgets window's pointer, as the window closes.
func (s *InputState) forgetPointer(window Window) {
	if index := s.findPointer(window); index >= 0 {
		s.pointers = slices.Delete(s.pointers, index, index+1)
	}
}

// pointer answers window's pointer, or the zero one if the window state does
// not know window. The zero Window is the only window known; it panics
// unless exactly one is.
func (s *InputState) pointer(window Window) windowPointer {
	if window == (Window{}) {
		if len(s.pointers) != 1 {
			panic(fmt.Sprintf("platform: the zero Window reads the only window, but %d are known", len(s.pointers)))
		}
		return s.pointers[0]
	}
	if index := s.findPointer(window); index >= 0 {
		return s.pointers[index]
	}
	return windowPointer{}
}

// findPointer answers the index of window's pointer, or -1 if the window
// state does not know window.
func (s *InputState) findPointer(window Window) int {
	return slices.IndexFunc(s.pointers, func(pointer windowPointer) bool { return pointer.window == window })
}

// windowPointer is the pointer as one window last saw it.
type windowPointer struct {
	window   Window
	position Point
	// placed is whether the pointer has entered or moved in the window, so
	// position is where it was.
	placed bool
	inside bool
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
