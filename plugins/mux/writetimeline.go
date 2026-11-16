package mux

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"time"
)

// writeTimeline is model M's writing side, which WriteTimeline names: a
// timelineSide with every write.
type writeTimeline[M any, D comparable] struct{ timelineSide[M, D] }

// Tween schedules a tween of the property path names, at the address holes
// fill: from from to to, eased by easing, over length from start on the
// timeline's clock. A start before Now begins the tween partway through. Step
// samples it from its next run on, and it leaves at the first sample at or
// past its end, where the model's own value shows again, so an update tweens
// to the value it set in the model.
//
// Overrides that overlap at one address blend, so a tween over a running one
// takes over with no jump. Each weighs nothing at its start and end, and
// fully at its midpoint, and the current value is their average by weight,
// moved through the property type's interpolator by EasingLinear. A short
// tween over a long one near its midpoint therefore shows about halfway
// between the two. Where every override weighs nothing, the one started last
// shows, and of those that start together, the one scheduled last.
//
// A length that is not positive, an easing that is not registered, a
// property type no interpolator tweens, or holes the path does not take,
// panics.
func (w writeTimeline[M, D]) Tween[T any](path Path[M, T], start time.Duration, from, to T, length time.Duration, easing Easing, holes ...int64) {
	if length <= 0 {
		panic(fmt.Sprintf("mux: path %q is tweened over %v, which is no positive length", path.compiled.text, length))
	}
	if w.state.easings.function(easing) == nil {
		panic(fmt.Sprintf("mux: path %q is tweened by easing %d, which is not registered", path.compiled.text, easing))
	}
	if _, ok := path.compiled.interpolator.(interpolator[T]); !ok {
		panic(fmt.Sprintf("mux: path %q is tweened, but no interpolator tweens its %v", path.compiled.text, path.compiled.propertyType))
	}
	scheduled := override[D, T]{start: start, length: length, from: from, to: to, easing: easing}
	path.compiled.fillHoles(&scheduled.holes, holes)
	w.state.scheduleOverride(path, scheduled)
}

// Hold schedules a hold of the property path names, at the address holes
// fill: its current value is value over length from start on the timeline's
// clock, as a tween from value to value eased by EasingLinear would show. A
// hold calls no interpolator to make its value, so a property of any type is
// held, a bool or a struct included. Step samples it from its next run on,
// and it leaves at the first sample at or past its end, as a tween does.
//
// A hold overlapping other overrides at the address blends with them as a
// tween does. On a type no interpolator tweens, the override that weighs most
// shows instead: the one nearest its midpoint; where they tie, the one
// started last; where they start together too, the one scheduled last.
//
// A length that is not positive, or holes the path does not take, panics.
func (w writeTimeline[M, D]) Hold[T any](path Path[M, T], start time.Duration, value T, length time.Duration, holes ...int64) {
	if length <= 0 {
		panic(fmt.Sprintf("mux: path %q is held over %v, which is no positive length", path.compiled.text, length))
	}
	scheduled := override[D, T]{start: start, length: length, from: value, to: value, easing: EasingLinear, isHold: true}
	path.compiled.fillHoles(&scheduled.holes, holes)
	w.state.scheduleOverride(path, scheduled)
}

// Call schedules function to be called back at start on the timeline's
// clock, given the model and the timeline's writing side, and answers the
// callback's index, which CancelCall takes. The timeline keeps function in
// its cache, and of the callback only start and the index, so it stays plain
// data: callbacks are not saved with it. function may capture anything,
// which is its own allocation; Call allocates nothing more once the timeline
// has grown.
//
// Step calls each callback once, after it samples and retires the
// overrides, at the first Step at or past its start, so one scheduled before
// Now is called at the next. Callbacks are called in time order, and those
// that start together in the order scheduled. A callback changes the model
// and schedules on the timeline as an update does, such as purging a unit
// whose fade has ended. Overrides it schedules show from the next Step, but a
// callback it schedules at or before Now is called in the same Step, in turn,
// so a callback that keeps scheduling another at Now never lets Step end.
//
// Once its callback is called or cancelled, an index is free, and the next
// Call may answer it for another callback.
//
// The callbacks still to come are kept sorted, so Call moves each one that
// comes due no later than start: it costs in proportion to them, little with
// tens pending and more with thousands.
//
// A nil function panics.
func (w writeTimeline[M, D]) Call(start time.Duration, function func(model *M, timeline WriteTimeline[M, D])) int {
	if function == nil {
		panic(fmt.Sprintf("mux: a nil function is called back at %v", start))
	}
	state := w.state
	var index int
	if lastIndex := len(state.freeCallbackIndices) - 1; lastIndex >= 0 {
		index = state.freeCallbackIndices[lastIndex]
		state.freeCallbackIndices = state.freeCallbackIndices[:lastIndex]
	} else {
		index = len(state.callbackCache)
		state.callbackCache = append(state.callbackCache, nil)
	}
	state.callbackCache[index] = function
	// The callbacks are kept latest first, so this one goes before every one
	// starting with it or earlier, which come due before it.
	insertionIndex := sort.Search(len(state.callbacks), func(callbackIndex int) bool { return state.callbacks[callbackIndex].start <= start })
	state.callbacks = slices.Insert(state.callbacks, insertionIndex, scheduledCallback{start: start, index: index})
	return index
}

// MakeSequence answers a sequence starting at start on the timeline's clock,
// which places overrides and callbacks one after another, or alongside each
// other, working out their starts, as an update shows a unit walk, wait and
// attack. A sequence started at End queues after whatever plays on an
// address.
func (w writeTimeline[M, D]) MakeSequence(start time.Duration) Sequence[M, D] {
	return Sequence[M, D]{timeline: w, previousStart: start, end: start}
}

// End answers when the address holes fill on path is next free: the end of
// the last of its overrides to end, or Now when it has none left or the last
// has ended. An override started at End therefore follows whatever plays at
// the address, and never starts in the past, and so does a sequence that
// MakeSequence starts there.
//
// End finds the address's overrides by binary search, so it costs in
// proportion to the log of the overrides on path. Those scheduled since the
// last Step it searches too, while they were scheduled in hole order, as an
// update moving units in index order schedules them; once one was scheduled
// out of order, it walks them, and costs in proportion to them until the next
// Step.
//
// Holes the path does not take panic, naming the path and their count.
func (w writeTimeline[M, D]) End[T any](path Path[M, T], holes ...int64) time.Duration {
	var filledHoles D
	path.compiled.fillHoles(&filledHoles, holes)
	end := w.state.now
	if state := w.state.findPathState(path); state != nil {
		state.visitOverrides(&filledHoles, func(scheduled *override[D, T]) { end = max(end, scheduled.start+scheduled.length) })
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
func (w writeTimeline[M, D]) Intern[K comparable](key K) int64 {
	return w.state.findKeyTable[K]("interned").intern(key)
}

// Cancel removes every override on the address holes fill on path, running
// or yet to start. From the next Step on, the address's current value is the
// model's own, which may jump.
//
// Cancel finds the address's overrides as End does, so it costs what End
// costs, and moves no other override: Step retires those it cancelled.
//
// Holes the path does not take panic, naming the path and their count.
func (w writeTimeline[M, D]) Cancel[T any](path Path[M, T], holes ...int64) {
	var filledHoles D
	path.compiled.fillHoles(&filledHoles, holes)
	if state := w.state.findPathState(path); state != nil {
		state.cancelOverrides(&filledHoles, w.state.now)
	}
}

// CancelCall drops the callback at index, an index Call answered, if it is
// still to come: Step never calls it, and its index is free for the next
// Call. An index whose callback was called or cancelled is free, so
// CancelCall drops nothing there, unless Call has since answered it for
// another callback, which it then drops: a game cancels only a callback it
// knows is still to come.
//
// CancelCall searches the callbacks still to come for index, so it costs in
// proportion to them.
//
// An index Call never answered panics.
func (w writeTimeline[M, D]) CancelCall(index int) {
	state := w.state
	if index < 0 || index >= len(state.callbackCache) {
		panic(fmt.Sprintf("mux: callback %d is cancelled, but Call never answered it", index))
	}
	if state.callbackCache[index] == nil {
		return
	}
	callbackIndex := slices.IndexFunc(state.callbacks, func(scheduled scheduledCallback) bool { return scheduled.index == index })
	state.callbacks = slices.Delete(state.callbacks, callbackIndex, callbackIndex+1)
	state.callbackCache[index] = nil
	state.freeCallbackIndices = append(state.freeCallbackIndices, index)
}

// Pause stops the timeline's time: Step leaves Now where it is until Resume,
// whatever its delta, so current values hold and no callback comes due, as
// when a menu is shown over the game. A paused Step still samples, so an
// override scheduled or cancelled while paused shows from the next Step, and
// a callback scheduled at or before Now is called there. Pausing a paused
// timeline changes nothing.
func (w writeTimeline[M, D]) Pause() { w.state.isPaused = true }

// Resume lets Step advance the timeline's time again after Pause, at the
// speed last set. Resuming a running timeline changes nothing.
func (w writeTimeline[M, D]) Resume() { w.state.isPaused = false }

// SetSpeed sets what Step multiplies its delta by before it advances the
// timeline's time: 4 plays an AI's turn four times faster, and 0 stops time as
// Pause does. The speed is 1 until set, and a paused timeline keeps the one
// set for its resume.
//
// A speed that is negative, infinite or not a number panics.
func (w writeTimeline[M, D]) SetSpeed(speed float64) {
	var reason string
	switch {
	case math.IsNaN(speed):
		reason = "not a number"
	case speed < 0:
		reason = "negative"
	case math.IsInf(speed, 1):
		reason = "infinite"
	}
	if reason != "" {
		panic(fmt.Sprintf("mux: model %v's timeline is set to speed %v, which is %s", reflect.TypeFor[M](), speed, reason))
	}
	w.state.speed = speed
}

// Skip moves Now to the end of the timeline's last override to end, or leaves
// it where it is when none ends later, as a game's "skip animations" does.
// The next Step samples there, so every override has finished and the
// model's own values show, and calls each callback Skip passed, in time
// order, as it calls any that came due: a unit whose fade was skipped is
// purged all the same. A callback after the last end stays to come, and one
// called schedules from the new Now, so overrides it schedules play on.
func (w writeTimeline[M, D]) Skip() {
	state := w.state
	for _, path := range state.pathStates {
		if path != nil {
			state.now = path.findLastEnd(state.now)
		}
	}
}
