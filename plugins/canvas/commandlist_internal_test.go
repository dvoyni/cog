package canvas

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
)

func TestArgumentsRecordedIntoTwoCommandListsMergeEachBesideItsEntry(t *testing.T) {
	// The first list records two sprites given 1 and 2 by one call, and a
	// sprite given none; the second a shape given 3: merged, each entry
	// answers its own argument's bytes, and the one given none, none.
	state := &drawState{inspector: &inspector{}, sortStopwatch: metronome.NewManualClock().NewStopwatch(SortStopwatchName)}
	first := state.addCommandList()
	second := state.addCommandList()
	state.recording.Store(true)
	first.record(state, EntryKindSprite, &first.sprites, reflect.TypeFor[float32](), castToBytes([]float32{1, 2}), Sprite{}, Sprite{})
	first.record(state, EntryKindSprite, &first.sprites, nil, nil, Sprite{})
	second.record(state, EntryKindShape, &second.shapes, reflect.TypeFor[float32](), castToBytes([]float32{3}), Shape{})

	state.closeAndSort()

	var got [][]float32
	for entry := range uint32(len(state.frameCommandList.entries)) {
		argument := state.frameCommandList.argument(entry)
		got = append(got, unsafe.Slice((*float32)(unsafe.Pointer(unsafe.SliceData(argument))), len(argument)/4))
	}
	want := [][]float32{{1}, {2}, {}, {3}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("the merged entries' arguments are %v, want %v", got, want)
	}
	state.recording.Store(true)
	second.record(state, EntryKindShape, &second.shapes, reflect.TypeFor[float32](), castToBytes([]float32{4}), Shape{})
	state.closeAndSort()
	if len(state.frameCommandList.arguments) != 4 {
		t.Errorf("the next frame, of one argument of 4 bytes, holds %d bytes of arguments, want the frame's alone", len(state.frameCommandList.arguments))
	}
}

func TestEachEntryAnswersTheSortKeyMaterialAndArgumentOfItsRecord(t *testing.T) {
	// An entry of each kind, a run of text naming no material drawn with
	// TextMaterial, the shape and the mesh each giving a float32.
	material := Material{index: 1, generation: 1}
	argumentType := reflect.TypeFor[float32]()
	list := commandList{
		entries: []recordedEntry{
			{kind: EntryKindShape, argument: recordedArgument{goType: argumentType}},
			{kind: EntryKindSprite},
			{kind: EntryKindText},
			{kind: EntryKindText, index: 1},
			{kind: EntryKindTriangles},
			{kind: EntryKindMesh, argument: recordedArgument{goType: argumentType, offset: 4}},
		},
		shapes:    []Shape{{Layer: 1, Order: 2, Material: material}},
		sprites:   []Sprite{{Layer: -3, Order: 4, Material: material}},
		texts:     []laidOutText{{text: Text{Layer: 5, Order: -6}}, {text: Text{Material: material}}},
		triangles: []recordedTriangles{{options: DrawTrianglesOptions{Layer: 7, Order: 8, Material: material}}},
		meshes:    []Mesh{{Layer: 9, Order: math.MinInt32, Material: material}},
		arguments: castToBytes([]float32{10, 11}),
	}

	for entry, want := range []struct {
		key      sortKey
		material Material
		argument []float32
	}{
		{makeSortKey(1, 2), material, []float32{10}},
		{makeSortKey(-3, 4), material, nil},
		{makeSortKey(5, -6), TextMaterial, nil},
		{makeSortKey(0, 0), material, nil},
		{makeSortKey(7, 8), material, nil},
		{makeSortKey(9, math.MinInt32), material, []float32{11}},
	} {
		recorded := list.entries[entry]
		if got := list.sortKey(uint32(entry)); got != want.key {
			t.Errorf("entry %d, %+v, answers the sort key %x, want its record's layer and order, %x", entry, recorded, got, want.key)
		}
		if got := list.material(uint32(entry)); got != want.material {
			t.Errorf("entry %d, %+v, answers the material %v, want %v", entry, recorded, got, want.material)
		}
		if got := list.argument(uint32(entry)); !slices.Equal(got, castToBytes(want.argument)) {
			t.Errorf("entry %d, %+v, answers the argument's bytes %v, want those of %v", entry, recorded, got, want.argument)
		}
	}
}

func TestAnEntrysMaterialSlotSkipsItForEachReasonItGives(t *testing.T) {
	floatType := reflect.TypeFor[float32]()
	materials := &frameMaterials{slots: []materialSlot{
		{},
		{generation: 1},
		{generation: 1, argumentType: floatType, stride: findInstanceStride(floatType), argumentLayoutMismatch: true},
		{generation: 1, argumentType: floatType, stride: findInstanceStride(floatType)},
		{generation: 1, ownVertexStage: true},
	}}
	plain := Material{index: 1, generation: 1}
	mismatched := Material{index: 2, generation: 1}
	takingFloat := Material{index: 3, generation: 1}
	ownVertexStage := Material{index: 4, generation: 1}
	released := Material{index: 1, generation: 2}
	newShape := func(material Material, argumentType reflect.Type) *commandList {
		return &commandList{
			entries:   []recordedEntry{{kind: EntryKindShape, argument: recordedArgument{goType: argumentType}}},
			shapes:    []Shape{{Rect: m.Rect{Width: 2, Height: 2}, Material: material}},
			arguments: make([]byte, findArgumentSize(argumentType)),
		}
	}
	newTriangles := func(material Material, standardLayout bool) *commandList {
		return &commandList{
			entries:   []recordedEntry{{kind: EntryKindTriangles}},
			triangles: []recordedTriangles{{options: DrawTrianglesOptions{Material: material}, standardLayout: standardLayout, bounds: m.Box3{Max: m.Vec3{X: 2, Y: 2}}}},
		}
	}

	for _, test := range []struct {
		name string
		list *commandList
		want skipReason
	}{
		{"a shape of a material released, its slot's generation past it", newShape(released, nil), skipReasonMaterialNotHeld},
		{"a shape of a material whose argument's layout mismatched its shader's", newShape(mismatched, nil), skipReasonArgumentLayoutMismatch},
		{"a shape giving a material taking a float32 an int32", newShape(takingFloat, reflect.TypeFor[int32]()), skipReasonArgumentRefused},
		{"a shape giving a material taking none a float32", newShape(plain, floatType), skipReasonArgumentRefused},
		{"a shape giving a material taking a float32 none", newShape(takingFloat, nil), skipReasonNone},
		{"a shape giving a material taking a float32 one", newShape(takingFloat, floatType), skipReasonNone},
		{"triangles of another layout than canvas.Vertex's, of a material of canvas's vertex stage", newTriangles(plain, false), skipReasonVertexStageMissing},
		{"triangles of another layout than canvas.Vertex's, of a material of its own vertex stage", newTriangles(ownVertexStage, false), skipReasonNone},
		{"triangles of canvas.Vertex's layout, of a material of canvas's vertex stage", newTriangles(plain, true), skipReasonNone},
	} {
		// The drawn entry is written over whole, the frame before's left
		// behind in none of its fields.
		got := drawnEntry{bounds: m.Rect{Width: 1}, key: batchKey{triangles: 1}, skip: skipReasonNoGlyphDrawn}
		test.list.findDrawnEntry(&got, 0, materials, newShapeAtlases(), nil, FilterDefault)

		if test.want != skipReasonNone {
			if want := (drawnEntry{skip: test.want}); got != want {
				t.Errorf("%s is drawn as %+v, want skipped for reason %d, its bounds and batch key zero", test.name, got, test.want)
			}
			continue
		}
		slot, _ := materials.find(test.list.material(0))
		if got.skip != skipReasonNone || got.bounds == (m.Rect{}) || got.key.material != test.list.material(0) || got.key.stride != slot.stride {
			t.Errorf("%s is drawn as %+v, want it drawn, of bounds, of its material and its slot's stride %d", test.name, got, slot.stride)
		}
	}
}
