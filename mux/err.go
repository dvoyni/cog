package mux

import "fmt"

// holeMismatchError is the panic of a path given a number of holes other than
// its own. It is a value, its message formatted only when read, so that the
// checks raising it stay cheap enough to inline, as makeHoles is into every
// Current and Tween.
type holeMismatchError struct {
	// path is the path given the holes.
	path *compiledPath
	// givenCount is how many holes it is given.
	givenCount int
}

// Error answers the message: the path, how many holes it has and how many it
// is given.
func (e holeMismatchError) Error() string {
	holes := "holes"
	if e.path.holeCount == 1 {
		holes = "hole"
	}
	return fmt.Sprintf("mux: path %q has %d %s, but is given %d", e.path.text, e.path.holeCount, holes, e.givenCount)
}
