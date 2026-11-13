package mux

import (
	"fmt"
	"math"
	"reflect"
	"time"
	"unsafe"

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
	// nothing was ever scheduled on. It grows as paths are first scheduled
	// on.
	pathStates []sampler
	// keyTables holds the table of each type of key that a map a path of
	// the model crosses is keyed by, other than an integer type. MakePath
	// fills it, and the model's registration holds it too.
	keyTables *[]*keyTable
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

// findKeyTable answers the table of keys of type K, for a key that is the
// subject of operation. A K that no map a path of the model crosses is keyed
// by, or that is an integer type, has none, and panics.
func (s *timelineState[M, D]) findKeyTable[K comparable](operation string) *keyTable {
	// Only K's table holds a map[K]int64, so asserting each table's finds
	// it, with no type hashed.
	for _, table := range *s.keyTables {
		if _, ok := table.indices.(map[K]int64); ok {
			return table
		}
	}
	keyType := reflect.TypeFor[K]()
	panic(fmt.Sprintf("mux: a %v key is %s, but no path of model %v crosses a map interning %v keys", keyType, operation, reflect.TypeFor[M](), keyType))
}

// findPathState answers path's state, or nil when nothing was ever scheduled
// on path.
func (s *timelineState[M, D]) findPathState[T any](path Path[M, T]) *pathState[D, T] {
	if path.compiled.id >= len(s.pathStates) {
		return nil
	}
	state, _ := s.pathStates[path.compiled.id].(*pathState[D, T])
	return state
}

// scheduleOverride adds scheduled to the overrides on path, making the path's
// state when it is the first.
func (s *timelineState[M, D]) scheduleOverride[T any](path Path[M, T], scheduled override[D, T]) {
	state := s.findPathState(path)
	if state == nil {
		for len(s.pathStates) <= path.compiled.id {
			s.pathStates = append(s.pathStates, nil)
		}
		// A path whose property no interpolator tweens takes only holds, which
		// call none.
		interpolate, _ := path.compiled.interpolator.(interpolator[T])
		state = &pathState[D, T]{interpolate: interpolate}
		s.pathStates[path.compiled.id] = state
	}
	state.addOverride(scheduled)
}

// walk yields each element of the last hole of path in model, the holes
// before it fixed by fixedHoles, with its hole and current value, as
// Timeline.All describes, until yield answers false. It is never inlined, so
// the walk All answers stays small enough to inline into a range over it.
//
//go:noinline
func (s *timelineState[M, D]) walk[T any](path Path[M, T], model *M, fixedHoles []int64, yield func(int64, T) bool) {
	compiled := path.compiled
	if compiled.holeCount == 0 {
		panic(fmt.Sprintf("mux: path %q has no hole for All to walk", compiled.text))
	}
	if len(fixedHoles) != compiled.holeCount-1 {
		holes := "holes"
		if compiled.holeCount == 1 {
			holes = "hole"
		}
		panic(fmt.Sprintf("mux: path %q has %d %s, so All takes %d fixed, but is given %d", compiled.text, compiled.holeCount, holes, compiled.holeCount-1, len(fixedHoles)))
	}
	beforeLastHole := compiled.beforeLastHole
	container, ok := beforeLastHole.findProperty(unsafe.Pointer(model), fixedHoles)
	if !ok {
		return
	}
	step := &compiled.steps[len(beforeLastHole.steps)]
	elements := unsafe.Add(container, step.offset)
	// Any key of a map may be a hole; an array's or a slice's holes run from
	// 0 to its length.
	firstHole := int64(math.MinInt64)
	lastHole := int64(math.MaxInt64)
	length := step.length
	if step.kind != reflect.Map {
		if step.kind == reflect.Slice {
			// Every slice's header is laid out alike, so it reads as a
			// []byte's.
			slice := *(*[]byte)(elements)
			elements = unsafe.Pointer(unsafe.SliceData(slice))
			length = int64(len(slice))
		}
		firstHole = 0
		lastHole = length - 1
	}
	// sampledRun is what the sample holds at the walked elements, sorted by
	// the last hole, which lies lastHoleOffset into an address.
	var sampledRun []sampledValue[D, T]
	lastHoleOffset := uintptr(len(fixedHoles)) * unsafe.Sizeof(int64(0))
	if state := s.findPathState(path); state != nil && len(state.sample) != 0 {
		sampledRun = state.findSampledRun(beforeLastHole.makeHoles[D](fixedHoles), lastHoleOffset, firstHole, lastHole)
	}
	// A path that ends at its last hole, as most do, reads each element's
	// property in place, with no steps to follow.
	afterLastHole := compiled.afterLastHole
	isPropertyInElement := len(afterLastHole.steps) == 0
	if step.kind == reflect.Map {
		// A MapIter held here stays on the stack, and so allocates nothing.
		var iterator reflect.MapIter
		iterator.Reset(reflect.NewAt(step.mapReflectType, elements).Elem())
		for iterator.Next() {
			// The entry is read in place, out of the MapIter's layout: the
			// runtime's iterator follows the map's Value in it, and the
			// iterator's first two fields point at the entry's key and
			// element, as the compiler's own range over a map reads them.
			// The layout lies outside Go's compatibility promise, but
			// MapIter's Key and Value copy each entry, and the iterator the
			// runtime keeps for packages outside it allocates on every start
			// (go.dev/issue/67401).
			entry := (*[2]unsafe.Pointer)(unsafe.Add(unsafe.Pointer(&iterator), unsafe.Sizeof(reflect.Value{})))
			hole, ok := step.findKeyHole(entry[0])
			if !ok {
				continue
			}
			value, ok := findSampledHole(sampledRun, lastHoleOffset, hole)
			if !ok {
				if isPropertyInElement {
					value = *(*T)(unsafe.Add(entry[1], afterLastHole.offset))
				} else if property, found := afterLastHole.findProperty(entry[1], nil); found {
					value = *(*T)(property)
				} else {
					continue
				}
			}
			if !yield(hole, value) {
				return
			}
		}
		return
	}
	// The run holds one entry for each of its holes, so the walk merges it
	// in index order, comparing the last hole alone.
	runIndex := 0
	for index := range length {
		var value T
		if runIndex < len(sampledRun) && *(*int64)(unsafe.Add(unsafe.Pointer(&sampledRun[runIndex].holes), lastHoleOffset)) == index {
			value = sampledRun[runIndex].value
			runIndex++
		} else {
			element := unsafe.Add(elements, uintptr(index)*step.elementSize)
			if isPropertyInElement {
				value = *(*T)(unsafe.Add(element, afterLastHole.offset))
			} else if property, found := afterLastHole.findProperty(element, nil); found {
				value = *(*T)(property)
			} else {
				continue
			}
		}
		if !yield(index, value) {
			return
		}
	}
}
