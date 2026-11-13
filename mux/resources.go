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
// the address holes fill: the value of the override active there in the
// sample the last Step took, and true, or path.Get's answer when none is. It
// reads that sample alone, so an override scheduled or cancelled since shows
// from the next Step.
//
// Holes the path does not take panic, naming the path and their count.
func (t Timeline[M, D]) Current[T any](path Path[M, T], model *M, holes ...int64) (T, bool) {
	// Get checks the holes on a path with nothing sampled, so only a path
	// with a sample makes them into an address.
	if state := t.state.findPathState(path); state != nil && len(state.sample) != 0 {
		filledHoles := path.compiled.makeHoles[D](holes)
		if value, ok := state.findSampledValue(&filledHoles); ok {
			return value, true
		}
	}
	return path.Get(model, holes...)
}

// Index answers the index the timeline interned key at, which is the hole
// that reaches key's element through a map keyed by K, and true. It answers
// false for a key never interned, whose element no path reaches. It writes
// nothing, so a view looks keys up under the read lock.
//
// A K that keys no map a path of M crosses, or that is an integer type, whose
// keys are their own holes, panics.
func (t Timeline[M, D]) Index[K comparable](key K) (int64, bool) {
	index, ok := t.state.findKeyTable[K]("looked up").indices.(map[K]int64)[key]
	return index, ok
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
	if _, ok := path.compiled.interpolator.(interpolator[T]); !ok {
		panic(fmt.Sprintf("mux: path %q is tweened, but no interpolator tweens its %v", path.compiled.text, path.compiled.propertyType))
	}
	w.state.scheduleOverride(path, override[D, T]{holes: path.compiled.makeHoles[D](holes), start: start, length: length, from: from, to: to, easing: easing})
}

// Hold schedules a hold of the property path names, at the address holes
// fill: its current value is value over length from start on the timeline's
// clock, as a tween from value to value eased by EasingLinear would show. A
// hold never calls an interpolator, so a property of any type is held, a
// bool or a struct included. Step samples it from its next run on, and it
// leaves at the first sample at or past its end, as a tween does.
//
// A length that is not positive, or holes the path does not take, panics.
func (w WriteTimeline[M, D]) Hold[T any](path Path[M, T], start time.Duration, value T, length time.Duration, holes ...int64) {
	if length <= 0 {
		panic(fmt.Sprintf("mux: path %q is held over %v, which is no positive length", path.compiled.text, length))
	}
	w.state.scheduleOverride(path, override[D, T]{holes: path.compiled.makeHoles[D](holes), start: start, length: length, from: value, to: value, easing: EasingLinear, isHold: true})
}

// End answers when the address holes fill on path is next free: the end of
// the last of its overrides to end, or Now when it has none left or the last
// has ended. An override started at End therefore follows whatever plays at
// the address, and never starts in the past.
//
// Holes the path does not take panic, naming the path and their count.
func (w WriteTimeline[M, D]) End[T any](path Path[M, T], holes ...int64) time.Duration {
	filledHoles := path.compiled.makeHoles[D](holes)
	end := w.state.now
	if state := w.state.findPathState(path); state != nil {
		for overrideIndex := range state.overrides {
			scheduled := &state.overrides[overrideIndex]
			if scheduled.holes == filledHoles {
				end = max(end, scheduled.start+scheduled.length)
			}
		}
	}
	return end
}

// Intern answers key's index in the timeline, which is the hole that reaches
// key's element through a map keyed by K: the index key was given first, or,
// for a key new to the timeline, the next of K's. An update interns the key
// of each element it adds to such a map, and a game every key after loading,
// since indices live one run. Path.Get reads the keys interned holding only
// the model's read lock, so a job interns holding the model's write lock too,
// as updates and callbacks do.
//
// A K that keys no map a path of M crosses, or that is an integer type, whose
// keys are their own holes, panics.
func (w WriteTimeline[M, D]) Intern[K comparable](key K) int64 {
	return w.state.findKeyTable[K]("interned").intern(key)
}

// Cancel removes every override on the address holes fill on path, running
// or yet to start. From the next Step on, the address's current value is the
// model's own, which may jump.
//
// Holes the path does not take panic, naming the path and their count.
func (w WriteTimeline[M, D]) Cancel[T any](path Path[M, T], holes ...int64) {
	filledHoles := path.compiled.makeHoles[D](holes)
	if state := w.state.findPathState(path); state != nil {
		state.keepOverrides(func(scheduled *override[D, T]) bool { return scheduled.holes != filledHoles })
	}
}
