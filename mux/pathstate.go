package mux

import (
	"slices"
	"time"
)

// sampler is a path's state as Step reaches it, whatever its property's type.
type sampler interface {
	// takeSample samples the path's overrides at now.
	takeSample(now time.Duration)
}

// pathState is what a timeline keeps of one path whose property is a T: the
// overrides scheduled on it, and their sample.
type pathState[D comparable, T any] struct {
	// overrides holds every override on the path that has not ended, in the
	// order scheduled.
	overrides []override[D, T]
	// sample holds the value of each address with an active override, as the
	// last Step evaluated it. It is empty on a path with none, so Current
	// skips the lookup there.
	sample []sampledValue[D, T]
	// interpolate is T's interpolator.
	interpolate interpolator[T]
}

// takeSample evaluates every override active at now into the sample, and
// retires those that have ended: an override leaves at the first sample at
// or past its end. Of two active at one address, the one scheduled last is
// sampled.
func (s *pathState[D, T]) takeSample(now time.Duration) {
	s.sample = s.sample[:0]
	keptCount := 0
	for overrideIndex := range s.overrides {
		scheduled := &s.overrides[overrideIndex]
		if now >= scheduled.start+scheduled.length {
			continue
		}
		if keptCount != overrideIndex {
			s.overrides[keptCount] = *scheduled
		}
		keptCount++
		if now < scheduled.start {
			continue
		}
		progress := float32(float64(now-scheduled.start) / float64(scheduled.length))
		value := s.interpolate(scheduled.from, scheduled.to, progress, easeLinear)
		sampleIndex := slices.IndexFunc(s.sample, func(sampled sampledValue[D, T]) bool { return sampled.holes == scheduled.holes })
		if sampleIndex >= 0 {
			s.sample[sampleIndex].value = value
		} else {
			s.sample = append(s.sample, sampledValue[D, T]{holes: scheduled.holes, value: value})
		}
	}
	s.overrides = s.overrides[:keptCount]
}

// override is one override on a path, as the timeline keeps it: plain data,
// with no func value in it.
type override[D comparable, T any] struct {
	// holes fill the path's holes: with the path, they are the override's
	// address.
	holes D
	// start is when the override starts, on the timeline's clock.
	start time.Duration
	// length is how long it lasts, always positive.
	length time.Duration
	// from and to are the values it moves between.
	from T
	to   T
	// easing is the number of the curve it moves by.
	easing Easing
}

// sampledValue is one address's value in its path's sample.
type sampledValue[D comparable, T any] struct {
	holes D
	value T
}
