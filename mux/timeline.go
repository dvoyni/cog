package mux

import (
	"fmt"
	"reflect"
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
