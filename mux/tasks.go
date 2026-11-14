package mux

import (
	"time"

	"github.com/dvoyni/cog/kernel"
)

// step is the body of Tasks.Step: it advances the timeline's time by delta
// times its speed, unless it is paused, samples every path's overrides at the
// new time, and then calls each callback that came due, in time order, given
// the model.
func step[M any, D comparable](_ *kernel.Kernel, delta time.Duration, modelHandle kernel.Write[*M], timelineHandle kernel.Write[Timeline[M, D]]) kernel.NoResult {
	state := timelineHandle.Get().state
	if !state.isPaused {
		state.now += time.Duration(float64(delta) * state.speed)
	}
	for _, path := range state.pathStates {
		if path != nil {
			path.takeSample(state.now, state.easings)
		}
	}
	// The callback leaves the schedule and the cache before it is called, so
	// the callbacks it schedules or cancels find both as they stand, and one
	// it schedules at or before now is called in turn.
	model := modelHandle.Get()
	for lastIndex := len(state.callbacks) - 1; lastIndex >= 0 && state.callbacks[lastIndex].start <= state.now; lastIndex = len(state.callbacks) - 1 {
		index := state.callbacks[lastIndex].index
		function := state.callbackCache[index]
		state.callbacks = state.callbacks[:lastIndex]
		state.callbackCache[index] = nil
		state.freeCallbackIndices = append(state.freeCallbackIndices, index)
		function(model, WriteTimeline[M, D]{timelineSide[M, D]{state}})
	}
	return kernel.NoResult{}
}
