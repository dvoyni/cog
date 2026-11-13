package mux

import (
	"sort"
	"time"
	"unsafe"
)

// sampler is a path's state as Step reaches it, whatever its property's type.
type sampler interface {
	// takeSample samples the path's overrides at now, each eased by its
	// function in easings.
	takeSample(now time.Duration, easings *easingTable)
}

// pathState is what a timeline keeps of one path whose property is a T: the
// overrides scheduled on it, and their sample.
type pathState[D comparable, T any] struct {
	// overrides holds every override on the path that has not ended, sorted
	// by holes and, at one address, in the order scheduled, once Step has
	// sorted them.
	overrides []override[D, T]
	// isUnsorted is whether an override was scheduled out of hole order
	// since the last Step, which then sorts the overrides.
	isUnsorted bool
	// sample holds the blend of each address's active overrides, sorted by
	// holes, as the last Step evaluated it. It is empty on a path with
	// none, so Current skips the lookup there.
	sample []sampledValue[D, T]
	// interpolate is T's interpolator, nil when T has none, so the path takes
	// only holds.
	interpolate interpolator[T]
}

// takeSample evaluates every override active at now into the sample, eased
// by its easing's function in easings, and retires those that have ended:
// an override leaves at the first sample at or past its end, where it weighs
// nothing. The overrides active at one address blend by weight, through the
// path's interpolator eased by EasingLinear's function. Where they cannot
// blend, because the path has no interpolator or none of them weighs
// anything yet, the one that weighs most shows; where weights tie, the one
// started last; where starts tie too, the one scheduled last.
func (s *pathState[D, T]) takeSample(now time.Duration, easings *easingTable) {
	if s.isUnsorted {
		// The overrides that have ended retire first, so the sort moves
		// only those that stay. A stable sort keeps the overrides at one
		// address in the order scheduled.
		s.keepOverrides(func(scheduled *override[D, T]) bool { return now < scheduled.start+scheduled.length })
		sort.Stable((*overrideHoleOrder[D, T])(&s.overrides))
		s.isUnsorted = false
	}
	s.sample = s.sample[:0]
	keptCount := 0
	for overrideIndex := 0; overrideIndex < len(s.overrides); overrideIndex++ {
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
		// The overrides at one address lie side by side, in the order
		// scheduled, so the sample comes out sorted by holes. An override
		// alone at its address, as most are, is sampled as it is: nothing of
		// a blend is kept live across its interpolator's call.
		if overrideIndex+1 == len(s.overrides) || compareHoles(&s.overrides[overrideIndex+1].holes, &scheduled.holes) != 0 {
			value := scheduled.from
			if !scheduled.isHold {
				value = s.interpolate(scheduled.from, scheduled.to, progress, easings.function(scheduled.easing))
			}
			s.sample = append(s.sample, sampledValue[D, T]{holes: scheduled.holes, value: value})
			continue
		}
		// Others follow at the address: they retire, wait or blend in here.
		// addressWeight is what the address's overrides weigh so far: their
		// sum where they blend, or the most any weighs. shownStart is the
		// start of the one whose value shows, unread once they blend. A
		// weight, 1 − |2p − 1| at progress p, is written so that it keeps its
		// precision near the start.
		holes := scheduled.holes
		addressWeight := 2 * min(progress, 1-progress)
		shownStart := scheduled.start
		value := scheduled.from
		if !scheduled.isHold {
			value = s.interpolate(scheduled.from, scheduled.to, progress, easings.function(scheduled.easing))
		}
		for overrideIndex+1 < len(s.overrides) && compareHoles(&s.overrides[overrideIndex+1].holes, &holes) == 0 {
			overrideIndex++
			otherOverride := &s.overrides[overrideIndex]
			if now >= otherOverride.start+otherOverride.length {
				continue
			}
			if keptCount != overrideIndex {
				s.overrides[keptCount] = *otherOverride
			}
			keptCount++
			if now < otherOverride.start {
				continue
			}
			otherProgress := float32(float64(now-otherOverride.start) / float64(otherOverride.length))
			weight := 2 * min(otherProgress, 1-otherProgress)
			otherValue := otherOverride.from
			if !otherOverride.isHold {
				otherValue = s.interpolate(otherOverride.from, otherOverride.to, otherProgress, easings.function(otherOverride.easing))
			}
			if s.interpolate != nil && addressWeight != 0 {
				// The fold of a normalized weighted average: each override
				// moves the value its share of the weight so far towards its
				// own.
				addressWeight += weight
				value = s.interpolate(value, otherValue, weight/addressWeight, easings.function(EasingLinear))
			} else if weight > addressWeight || weight == addressWeight && otherOverride.start >= shownStart {
				value = otherValue
				addressWeight = weight
				shownStart = otherOverride.start
			}
		}
		s.sample = append(s.sample, sampledValue[D, T]{holes: holes, value: value})
	}
	s.overrides = s.overrides[:keptCount]
}

// addOverride adds scheduled after the path's overrides, marking them for
// Step to sort when its holes sort before the last's.
func (s *pathState[D, T]) addOverride(scheduled override[D, T]) {
	if overrideCount := len(s.overrides); overrideCount != 0 && compareHoles(&scheduled.holes, &s.overrides[overrideCount-1].holes) < 0 {
		s.isUnsorted = true
	}
	s.overrides = append(s.overrides, scheduled)
}

// findSampledValue answers the value sampled at the address holes fill, and
// true, or false when no override was active there at the last Step.
func (s *pathState[D, T]) findSampledValue(holes *D) (T, bool) {
	// A binary search, comparing each address in place and stopping at
	// holes', where sort.Search would go on to the end of the range.
	lowIndex := 0
	highIndex := len(s.sample)
	for lowIndex < highIndex {
		middleIndex := int(uint(lowIndex+highIndex) >> 1)
		middleOrder := compareHoles(&s.sample[middleIndex].holes, holes)
		if middleOrder < 0 {
			lowIndex = middleIndex + 1
		} else if middleOrder > 0 {
			highIndex = middleIndex
		} else {
			return s.sample[middleIndex].value, true
		}
	}
	var zero T
	return zero, false
}

// keepOverrides keeps the overrides isKept answers true for, in the order
// they had, and removes the others.
func (s *pathState[D, T]) keepOverrides(isKept func(scheduled *override[D, T]) bool) {
	keptCount := 0
	for overrideIndex := range s.overrides {
		scheduled := &s.overrides[overrideIndex]
		if !isKept(scheduled) {
			continue
		}
		if keptCount != overrideIndex {
			s.overrides[keptCount] = *scheduled
		}
		keptCount++
	}
	// The overrides removed are cleared, so the values they hold are not
	// kept alive.
	clear(s.overrides[keptCount:])
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
	// isHold is whether the override is a hold, whose value is from
	// throughout, with no interpolator called to make it.
	isHold bool
}

// sampledValue is one address's value in its path's sample.
type sampledValue[D comparable, T any] struct {
	holes D
	value T
}

// compareHoles orders two addresses on one path by their holes, the first
// hole before the second: negative when firstHoles come before secondHoles,
// zero when they are one address, positive when secondHoles come before
// firstHoles. D is an [N]int64, as RegisterModel checks.
func compareHoles[D comparable](firstHoles, secondHoles *D) int {
	// The first hole is compared before the loop, which a depth of one hole
	// then leaves out. It is a loop, not slices.Compare over unsafe.Slice,
	// so it inlines.
	firstHole := *(*int64)(unsafe.Pointer(firstHoles))
	secondHole := *(*int64)(unsafe.Pointer(secondHoles))
	if firstHole != secondHole {
		if firstHole < secondHole {
			return -1
		}
		return 1
	}
	for holeOffset := unsafe.Sizeof(firstHole); holeOffset < unsafe.Sizeof(*firstHoles); holeOffset += unsafe.Sizeof(firstHole) {
		firstHole = *(*int64)(unsafe.Add(unsafe.Pointer(firstHoles), holeOffset))
		secondHole = *(*int64)(unsafe.Add(unsafe.Pointer(secondHoles), holeOffset))
		if firstHole != secondHole {
			if firstHole < secondHole {
				return -1
			}
			return 1
		}
	}
	return 0
}
