package mux

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// Tasks is the resource holding the tasks of model M's timeline, installed by
// RegisterModel[M, D] and read in a later plugin's Init with
// configurator.Resource[mux.Tasks[M, D]]().
type Tasks[M any, D comparable] struct {
	// Step advances the timeline by the delta it is given and samples every
	// active override at the new time, holding the write locks on *M and
	// Timeline[M, D]. mux schedules it nowhere: its user does, before the
	// jobs that read the timeline, as a game steps once per rendered frame
	// with the frame's delta, before its views.
	Step *kernel.Task[time.Duration, kernel.NoResult]
}

// Timeline is the resource of model M's timeline, installed by
// RegisterModel[M, D]: the reading side, which a job reads under
// kernel.Read[mux.Timeline[M, D]], as views do. It holds one private pointer,
// so Get copies a word, and nothing reached from it writes.
type Timeline[M any, D comparable] struct{ timelineSide[M, D] }

// Current answers the current value of the property path names in model, at
// the address holes fill: the value of the tween active there in the sample
// the last Step took, and true, or path.Get's answer when none is. It reads
// that sample alone, so a tween scheduled since shows from the next Step.
//
// Holes the path does not take panic, naming the path and their count.
func (t Timeline[M, D]) Current[T any](path Path[M, T], model *M, holes ...int64) (T, bool) {
	if path.compiled.id < len(t.state.pathStates) {
		// Get checks the holes on a path with nothing sampled, so only a path
		// with a sample makes them into an address.
		if state, ok := t.state.pathStates[path.compiled.id].(*pathState[D, T]); ok && len(state.sample) != 0 {
			filledHoles := path.compiled.makeHoles[D](holes)
			for _, sampled := range state.sample {
				if sampled.holes == filledHoles {
					return sampled.value, true
				}
			}
		}
	}
	return path.Get(model, holes...)
}

// ElevateTimeline answers the timeline a job holds the write lock on as its
// writing side. Only the write handle gives it, so a job holding the read
// lock cannot write.
func ElevateTimeline[M any, D comparable](handle kernel.Write[Timeline[M, D]]) WriteTimeline[M, D] {
	return WriteTimeline[M, D]{handle.Get().timelineSide}
}

// WriteTimeline is the writing side of model M's timeline, which
// ElevateTimeline gives; it is no resource.
type WriteTimeline[M any, D comparable] struct{ timelineSide[M, D] }

// Tween schedules a tween of the property path names, at the address holes
// fill: from from to to, eased by easing, over length from start on the
// timeline's clock. A start before Now begins the tween partway through. Step
// samples it from its next run on, and it leaves at the first sample at or
// past its end, where the model's own value shows again, so an update tweens
// to the value it set in the model.
//
// A length that is not positive, an easing that is not registered, a
// property type no interpolator tweens, or holes the path does not take,
// panics.
func (w WriteTimeline[M, D]) Tween[T any](path Path[M, T], start time.Duration, from, to T, length time.Duration, easing Easing, holes ...int64) {
	if length <= 0 {
		panic(fmt.Sprintf("mux: path %q is tweened over %v, which is no positive length", path.compiled.text, length))
	}
	if w.state.easings.function(easing) == nil {
		panic(fmt.Sprintf("mux: path %q is tweened by easing %d, which is not registered", path.compiled.text, easing))
	}
	interpolate, ok := path.compiled.interpolator.(interpolator[T])
	if !ok {
		panic(fmt.Sprintf("mux: path %q is tweened, but no interpolator tweens its %v", path.compiled.text, path.compiled.propertyType))
	}
	filledHoles := path.compiled.makeHoles[D](holes)
	id := path.compiled.id
	for len(w.state.pathStates) <= id {
		w.state.pathStates = append(w.state.pathStates, nil)
	}
	state, ok := w.state.pathStates[id].(*pathState[D, T])
	if !ok {
		state = &pathState[D, T]{interpolate: interpolate}
		w.state.pathStates[id] = state
	}
	state.overrides = append(state.overrides, override[D, T]{holes: filledHoles, start: start, length: length, from: from, to: to, easing: easing})
}
