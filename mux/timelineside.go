package mux

import "time"

// timelineSide is what both sides of a timeline are, Timeline and
// WriteTimeline: a pointer to the timeline's state, and the reads both offer.
type timelineSide[M any, D comparable] struct {
	state *timelineState[M, D]
}

// Now answers the timeline's current time, on its own clock: the sum of the
// deltas Step has advanced it by, each times the speed then set, and of the
// jumps Skip has made.
func (s timelineSide[M, D]) Now() time.Duration { return s.state.now }
