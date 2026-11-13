package mux

import (
	"time"

	"github.com/dvoyni/cog/kernel"
)

// timelineSide is what both sides of a timeline are, Timeline and
// WriteTimeline: a pointer to the timeline's state, and the reads both offer.
type timelineSide[M any, D comparable] struct {
	state *timelineState[M, D]
}

// Now answers the timeline's current time, on its own clock: the sum of the
// deltas Step has advanced it by.
func (s timelineSide[M, D]) Now() time.Duration { return s.state.now }

// timelineState is what a model's timeline keeps, which both its sides point
// to.
type timelineState[M any, D comparable] struct {
	now time.Duration
	// easings is the kernel's easings, which Step eases each override by.
	easings *easingTable
	// pathStates holds each path's state by the path's id, nil for a path
	// never tweened. It grows as paths are first tweened.
	pathStates []sampler
}

// step is the body of Tasks.Step: it advances the timeline's time by delta,
// then samples every path's overrides at the new time. modelHandle reads
// nothing yet; it declares the write lock on the model that Step holds.
func step[M any, D comparable](_ *kernel.Kernel, delta time.Duration, modelHandle kernel.Write[*M], timelineHandle kernel.Write[Timeline[M, D]]) kernel.NoResult {
	state := timelineHandle.Get().state
	state.now += delta
	for _, path := range state.pathStates {
		if path != nil {
			path.takeSample(state.now, state.easings)
		}
	}
	return kernel.NoResult{}
}
