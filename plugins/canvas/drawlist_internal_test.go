package canvas

import (
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/plugins/metronome"
)

func TestArgumentsRecordedIntoTwoDrawListsMergeEachBesideItsEntry(t *testing.T) {
	// The first list records two sprites given 1 and 2 by one call, and a
	// sprite given none; the second a shape given 3: merged, each entry
	// answers its own argument's bytes, and the one given none, none.
	state := &drawState{inspector: &inspector{}, sortStopwatch: metronome.NewManualClock().NewStopwatch(SortStopwatchName)}
	first := state.addDrawList()
	second := state.addDrawList()
	state.recording.Store(true)
	first.record(state, EntryKindSprite, &first.sprites, reflect.TypeFor[float32](), castToBytes([]float32{1, 2}), Sprite{}, Sprite{})
	first.record(state, EntryKindSprite, &first.sprites, nil, nil, Sprite{})
	second.record(state, EntryKindShape, &second.shapes, reflect.TypeFor[float32](), castToBytes([]float32{3}), Shape{})

	state.closeAndSort()

	var got [][]float32
	for _, entry := range state.entries {
		argument := state.findEntryArgument(entry)
		got = append(got, unsafe.Slice((*float32)(unsafe.Pointer(unsafe.SliceData(argument))), len(argument)/4))
	}
	want := [][]float32{{1}, {2}, {}, {3}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("the merged entries' arguments are %v, want %v", got, want)
	}
	state.recording.Store(true)
	second.record(state, EntryKindShape, &second.shapes, reflect.TypeFor[float32](), castToBytes([]float32{4}), Shape{})
	state.closeAndSort()
	if len(state.arguments) != 4 {
		t.Errorf("the next frame, of one argument of 4 bytes, holds %d bytes of arguments, want the frame's alone", len(state.arguments))
	}
}
