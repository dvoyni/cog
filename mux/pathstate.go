package mux

import (
	"math"
	"slices"
	"sort"
	"time"
	"unsafe"
)

// sampler is a path's state as Step reaches it, whatever its property's type.
type sampler interface {
	// takeSample samples the path's overrides at now, each eased by its
	// function in easings.
	takeSample(now time.Duration, easings *easingTable)
	// findLastEnd answers the end of the path's last override to end, or
	// end when that is later.
	findLastEnd(end time.Duration) time.Duration
}

// pathState is what a timeline keeps of one path whose property is a T: the
// overrides scheduled on it, and their sample.
type pathState[D comparable, T any] struct {
	// overrides holds every override on the path that has not ended. The
	// first sortedCount are sorted by holes and, at one address, in the order
	// scheduled, as the last sort left them. The rest, the tail, were
	// scheduled since, in the order scheduled.
	overrides   []override[D, T]
	sortedCount int
	// isTailUnsorted is whether the tail was scheduled out of hole order.
	isTailUnsorted bool
	// cancelledCount is how many overrides Cancel ended since the last pass
	// retired those that ended.
	cancelledCount int
	// sample holds the blend of each address's active overrides, sorted by
	// holes, as the last Step evaluated it. It is empty on a path with
	// none, so Current skips the lookup there. sampleScale is its scale by
	// the first hole, as measureSampleScale answers it, which Current's
	// search guesses by.
	sample      []sampledValue[D, T]
	sampleScale float64
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
	// The overrides need sorting when the tail is unsorted, or starts before
	// the sorted ones end.
	tail := s.overrides[s.sortedCount:]
	isTailBeforeSortedEnd := len(tail) != 0 && s.sortedCount != 0 && compareHoles(&tail[0].holes, &s.overrides[s.sortedCount-1].holes) < 0
	if len(tail) != 0 && (s.isTailUnsorted || isTailBeforeSortedEnd) {
		// The overrides that have ended retire first, so the sort moves
		// only those that stay. A stable sort keeps the overrides at one
		// address in the order scheduled.
		s.retireOverrides(now)
		sort.Stable((*overrideHoleOrder[D, T])(&s.overrides))
	}
	// The sample holds at most one entry per override, so it grows here, not
	// in the pass. Each entry is written in place: one built aside and
	// copied in is read back before its writes have landed, which stalls.
	s.sample = slices.Grow(s.sample[:0], len(s.overrides))[:len(s.overrides)]
	sampleCount := 0
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
			entry := &s.sample[sampleCount]
			sampleCount++
			entry.holes = scheduled.holes
			entry.value = value
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
		entry := &s.sample[sampleCount]
		sampleCount++
		entry.holes = holes
		entry.value = value
	}
	s.sample = s.sample[:sampleCount]
	s.sampleScale = measureSampleScale(s.sample, 0)
	s.overrides = s.overrides[:keptCount]
	s.sortedCount = keptCount
	s.isTailUnsorted = false
	s.cancelledCount = 0
}

// findLastEnd answers the end of the last of the path's overrides to end, or
// end when that is later.
func (s *pathState[D, T]) findLastEnd(end time.Duration) time.Duration {
	for overrideIndex := range s.overrides {
		scheduled := &s.overrides[overrideIndex]
		end = max(end, scheduled.start+scheduled.length)
	}
	return end
}

// addOverride adds scheduled to the tail, marking the tail unsorted when its
// holes sort before the tail's last.
func (s *pathState[D, T]) addOverride(scheduled override[D, T]) {
	if overrideCount := len(s.overrides); overrideCount > s.sortedCount && compareHoles(&scheduled.holes, &s.overrides[overrideCount-1].holes) < 0 {
		s.isTailUnsorted = true
	}
	s.overrides = append(s.overrides, scheduled)
}

// cancelOverrides ends every override at the address holes fill that has not
// ended at now. Each is left in place, ended at the start of time, so the
// pass that next retires the overrides that have ended retires it too: no
// override after it moves. Once those cancelled outnumber the rest, as when
// a game cancels most of a path between Steps, they retire at once.
func (s *pathState[D, T]) cancelOverrides(holes *D, now time.Duration) {
	s.visitOverrides(holes, func(scheduled *override[D, T]) {
		if now < scheduled.start+scheduled.length {
			scheduled.start = math.MinInt64
			scheduled.length = 1
			s.cancelledCount++
		}
	})
	if 2*s.cancelledCount > len(s.overrides) {
		s.retireOverrides(now)
	}
}

// findSampledValue answers the value sampled at the address holes fill, and
// true, or false when no override was active there at the last Step.
func (s *pathState[D, T]) findSampledValue(holes *D) (T, bool) {
	if entryIndex, ok := searchSample(s.sample, holes, 0, s.sampleScale); ok {
		return s.sample[entryIndex].value, true
	}
	var zero T
	return zero, false
}

// retireOverrides removes the overrides that have ended at now, keeping the
// others in the order they had.
func (s *pathState[D, T]) retireOverrides(now time.Duration) {
	keptCount := 0
	sortedKeptCount := 0
	for overrideIndex := range s.overrides {
		scheduled := &s.overrides[overrideIndex]
		if now >= scheduled.start+scheduled.length {
			continue
		}
		if keptCount != overrideIndex {
			s.overrides[keptCount] = *scheduled
		}
		keptCount++
		if overrideIndex < s.sortedCount {
			sortedKeptCount = keptCount
		}
	}
	// The overrides removed are cleared, so the values they hold are not
	// kept alive.
	clear(s.overrides[keptCount:])
	s.overrides = s.overrides[:keptCount]
	s.sortedCount = sortedKeptCount
	s.cancelledCount = 0
	if s.sortedCount == keptCount {
		s.isTailUnsorted = false
	}
}

// visitOverrides calls visit with each override at the address holes fill.
// It finds those sorted by binary search, and those in the tail by binary
// search too, unless the tail is unsorted, when it walks the tail.
func (s *pathState[D, T]) visitOverrides(holes *D, visit func(scheduled *override[D, T])) {
	sorted := s.overrides[:s.sortedCount]
	for overrideIndex := sort.Search(len(sorted), func(index int) bool { return compareHoles(&sorted[index].holes, holes) >= 0 }); overrideIndex < len(sorted) && compareHoles(&sorted[overrideIndex].holes, holes) == 0; overrideIndex++ {
		visit(&sorted[overrideIndex])
	}
	tail := s.overrides[s.sortedCount:]
	overrideIndex := 0
	if !s.isTailUnsorted {
		overrideIndex = sort.Search(len(tail), func(index int) bool { return compareHoles(&tail[index].holes, holes) >= 0 })
	}
	for ; overrideIndex < len(tail); overrideIndex++ {
		if order := compareHoles(&tail[overrideIndex].holes, holes); order == 0 {
			visit(&tail[overrideIndex])
		} else if order > 0 && !s.isTailUnsorted {
			return
		}
	}
}

// findSampledRun answers the run of the sample whose addresses are
// fixedAddress but for their hole at lastHoleOffset, which lies from
// firstHole to lastHole: the sample is sorted by holes, so All walks the run
// sorted by that hole.
func (s *pathState[D, T]) findSampledRun(fixedAddress D, lastHoleOffset uintptr, firstHole, lastHole int64) []sampledValue[D, T] {
	sample := s.sample
	lastHolePlace := (*int64)(unsafe.Add(unsafe.Pointer(&fixedAddress), lastHoleOffset))
	*lastHolePlace = firstHole
	run := sample[sort.Search(len(sample), func(index int) bool { return compareHoles(&sample[index].holes, &fixedAddress) >= 0 }):]
	*lastHolePlace = lastHole
	return run[:sort.Search(len(run), func(index int) bool { return compareHoles(&run[index].holes, &fixedAddress) > 0 })]
}

// override is one override on a path, as the timeline keeps it: plain data,
// with no func value in it.
type override[D comparable, T any] struct {
	// holes fill the path's holes: with the path, they are the override's
	// address.
	holes D
	// start is when the override starts, on the timeline's clock: the start
	// of time, math.MinInt64, once Cancel has ended it.
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

// measureSampleScale answers the scale of run, entries of a path's sample
// sorted by holes along which the hole keyOffset into an address rises: how
// many of its entries a rise of one in that hole spans, on average. It is 0
// where that hole never rises, as on a run of fewer than two entries.
func measureSampleScale[D comparable, T any](run []sampledValue[D, T], keyOffset uintptr) float64 {
	if len(run) < 2 {
		return 0
	}
	firstKey := *(*int64)(unsafe.Add(unsafe.Pointer(&run[0].holes), keyOffset))
	lastKey := *(*int64)(unsafe.Add(unsafe.Pointer(&run[len(run)-1].holes), keyOffset))
	if lastKey == firstKey {
		return 0
	}
	// A float64 holds the difference of any two int64s, if not exactly.
	return float64(len(run)-1) / (float64(lastKey) - float64(firstKey))
}

// searchSample answers the index of the entry at the address holes fill in
// run, entries of a path's sample sorted by holes along which the hole
// keyOffset into an address rises, and true, or false when run holds none
// there. scale is run's scale by that hole, as measureSampleScale answers it.
//
// It guesses where the entry lies from where its hole falls between the
// first entry's and the last's, and searches out from there, doubling its
// stride, before a binary search between the last two entries it compared.
// Where the holes sampled spread evenly, as over a slice's units with every
// fourth animated, the guess lands on the entry or beside it; at worst, it
// costs twice a binary search.
func searchSample[D comparable, T any](run []sampledValue[D, T], holes *D, keyOffset uintptr, scale float64) (int, bool) {
	if len(run) == 0 {
		return 0, false
	}
	key := *(*int64)(unsafe.Add(unsafe.Pointer(holes), keyOffset))
	firstKey := *(*int64)(unsafe.Add(unsafe.Pointer(&run[0].holes), keyOffset))
	guessIndex := 0
	// The guess is clamped to run while a float64, so no conversion to an
	// int overflows.
	if position := (float64(key) - float64(firstKey)) * scale; position > 0 {
		guessIndex = int(min(position, float64(len(run)-1)))
	}
	order := compareHoles(&run[guessIndex].holes, holes)
	if order == 0 {
		return guessIndex, true
	}
	// The entry lies at or after lowIndex and before highIndex.
	var lowIndex, highIndex int
	if order < 0 {
		lowIndex = guessIndex + 1
		for stride := 1; ; stride *= 2 {
			highIndex = guessIndex + stride
			if highIndex >= len(run) {
				highIndex = len(run)
				break
			}
			order = compareHoles(&run[highIndex].holes, holes)
			if order == 0 {
				return highIndex, true
			}
			if order > 0 {
				break
			}
			lowIndex = highIndex + 1
		}
	} else {
		highIndex = guessIndex
		for stride := 1; ; stride *= 2 {
			lowIndex = guessIndex - stride
			if lowIndex < 0 {
				lowIndex = 0
				break
			}
			order = compareHoles(&run[lowIndex].holes, holes)
			if order == 0 {
				return lowIndex, true
			}
			if order < 0 {
				lowIndex++
				break
			}
			highIndex = lowIndex
		}
	}
	// A binary search, comparing each address in place and stopping at
	// holes', where sort.Search would go on to the end of the range.
	for lowIndex < highIndex {
		middleIndex := int(uint(lowIndex+highIndex) >> 1)
		order = compareHoles(&run[middleIndex].holes, holes)
		if order < 0 {
			lowIndex = middleIndex + 1
		} else if order > 0 {
			highIndex = middleIndex
		} else {
			return middleIndex, true
		}
	}
	return 0, false
}
