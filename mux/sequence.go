package mux

import (
	"fmt"
	"time"
)

// Sequence places overrides and callbacks on a timeline one after another,
// or alongside each other, working out each one's start as it is placed, as
// WriteTimeline.MakeSequence gives it. It is a builder held on the stack,
// storing nothing of its own: what it places are ordinary overrides and
// callbacks, with absolute starts, scheduled as it places them.
type Sequence[M any, D comparable] struct {
	timeline writeTimeline[M, D]
	// previousStart is the start of what was placed last, or the
	// sequence's start before anything is.
	previousStart time.Duration
	// end is when everything placed so far has ended, the waits after it
	// included: what is placed next starts there.
	end time.Duration
}

// Tween schedules a tween as WriteTimeline.Tween does, starting after
// everything placed so far.
//
// It panics as WriteTimeline.Tween does.
func (s *Sequence[M, D]) Tween[T any](path Path[M, T], from, to T, length time.Duration, easing Easing, holes ...int64) {
	s.timeline.Tween(path, s.end, from, to, length, easing, holes...)
	s.previousStart = s.end
	s.end += length
}

// With schedules a tween as WriteTimeline.Tween does, starting alongside
// what was placed last, or at the sequence's start when nothing was, as a
// unit turns while it sets off on a walk. When it ends last, what is placed
// next starts after it.
//
// It panics as WriteTimeline.Tween does.
func (s *Sequence[M, D]) With[T any](path Path[M, T], from, to T, length time.Duration, easing Easing, holes ...int64) {
	s.timeline.Tween(path, s.previousStart, from, to, length, easing, holes...)
	s.end = max(s.end, s.previousStart+length)
}

// Hold schedules a hold as WriteTimeline.Hold does, starting after
// everything placed so far.
//
// It panics as WriteTimeline.Hold does.
func (s *Sequence[M, D]) Hold[T any](path Path[M, T], value T, length time.Duration, holes ...int64) {
	s.timeline.Hold(path, s.end, value, length, holes...)
	s.previousStart = s.end
	s.end += length
}

// Call schedules function to be called back as WriteTimeline.Call does,
// after everything placed so far, and answers the callback's index. A
// callback has no length, so what is placed next starts with it.
//
// It panics as WriteTimeline.Call does.
func (s *Sequence[M, D]) Call(function func(model *M, timeline WriteTimeline[M, D])) int {
	index := s.timeline.Call(s.end, function)
	s.previousStart = s.end
	return index
}

// Wait leaves a gap of length after everything placed so far, before what is
// placed next. A With after it still starts alongside what was placed last.
//
// A negative length panics.
func (s *Sequence[M, D]) Wait(length time.Duration) {
	if length < 0 {
		panic(fmt.Sprintf("mux: a sequence waits %v, which is no gap", length))
	}
	s.end += length
}
