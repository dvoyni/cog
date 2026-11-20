package canvas

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// DrawList is the task parameter a recording task records its entries
// through, declared in its signature as kernel.Read is:
//
//	func drawUnits(k *kernel.Kernel, event canvas.FrameEvent, list canvas.DrawList) kernel.NoResult
//
// Each task declaring it gets a draw list of its own at registration, so
// tasks recording together never contend; the lists are merged when the
// frame is drawn, in registration order: plugin order, then the order each
// plugin registers its tasks. Two jobs of one task in a frame record into
// its list both, in the order their calls run. It locks canvas's draw state
// for read, so recording tasks run together. It is valid only while the
// task's job runs.
//
// An entry is recorded, and a camera set, only from FrameEvent's Ready until
// canvas's drawing begins; one recorded or set at any other time is
// refused, and reported once for the task, as
// ReportKindRecordedOutsideFrame.
//
// An entry gives its material an argument with the …With calls, of the type
// the material declares with ArgumentOf, checked as canvas draws the frame:
// an argument of another type, or one given a material that takes none, is
// refused, reported once for the material and the type as
// ReportKindArgumentRefused, and the entry skipped. An entry recorded
// without one gives a material that takes one the zero value of its type.
type DrawList struct {
	state kernel.Read[*drawState]
	list  *drawList
}

// Declare declares DrawList's lock, gives the task being registered a draw
// list of its own, after every list given before, and answers DrawList
// bound to it.
func (DrawList) Declare(access *kernel.Access) DrawList {
	state := access.Read[*drawState]()
	return DrawList{state: state, list: state.Get().addDrawList()}
}

// DrawShape records shape into the task's draw list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l DrawList) DrawShape(shape Shape) {
	l.list.record(l.state.Get(), EntryKindShape, &l.list.shapes, nil, nil, shape)
}

// DrawShapeWith records shape into the task's draw list for the frame being
// recorded, as DrawShape does, giving its material argument, of the type
// the material declares with ArgumentOf.
func (l DrawList) DrawShapeWith[T any](shape Shape, argument T) {
	l.list.record(l.state.Get(), EntryKindShape, &l.list.shapes, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)), shape)
}

// DrawSprite records sprite into the task's draw list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l DrawList) DrawSprite(sprite Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, nil, nil, sprite)
}

// DrawSpriteWith records sprite into the task's draw list for the frame
// being recorded, as DrawSprite does, giving its material argument, of the
// type the material declares with ArgumentOf.
func (l DrawList) DrawSpriteWith[T any](sprite Sprite, argument T) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)), sprite)
}

// DrawSprites records sprites into the task's draw list for the frame being
// recorded, in their order, as DrawSprite would one after another, so an
// ECS chunk's column goes in as it is. Outside the frame, they are refused
// and reported once for the task.
func (l DrawList) DrawSprites(sprites []Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, nil, nil, sprites...)
}

// DrawSpritesWith records sprites into the task's draw list for the frame
// being recorded, as DrawSprites does, each giving its material the
// argument at its index in arguments, a parallel slice, so an ECS chunk's
// sprite and argument columns go in as they are. Slices of different
// lengths are refused, none of them recorded, and reported once for the
// task as ReportKindSliceLengthsDiffer.
func (l DrawList) DrawSpritesWith[T any](sprites []Sprite, arguments []T) {
	state := l.state.Get()
	if len(sprites) != len(arguments) {
		state.logger.LogErrOnce(l.list.slicesDiffer.Error(), l.list.slicesDiffer)
		return
	}
	l.list.record(state, EntryKindSprite, &l.list.sprites, reflect.TypeFor[T](), castToBytes(arguments), sprites...)
}

// SetCamera sets camera for the frame being recorded, forgotten once the
// frame is drawn, as its entries are: nothing is made or kept. A frame that
// sets no camera is seen through the default one, which draws only a frame
// with entries: every layer, into the whole main window, cleared to opaque
// black, at order 0. Outside the frame, it is refused and reported once for
// the task. A camera of a sample count other than 0, 1 and 4 panics.
func (l DrawList) SetCamera(camera Camera) {
	if camera.SampleCount > 1 && camera.SampleCount != multisampledCameraSampleCount {
		panic(fmt.Sprintf("canvas: a camera's SampleCount is 0, 1 or %d, not %d", multisampledCameraSampleCount, camera.SampleCount))
	}
	l.list.record(l.state.Get(), 0, &l.list.cameras, nil, nil, camera)
}

// drawList is one task's draw list for the frame being recorded, which its
// DrawList records into: the entries its jobs record, in the order
// recorded, each of them a shape or a sprite of its records of that kind,
// with the bytes of the material arguments they give, end to end, and the
// cameras they set, in the order set. Two jobs of one task running together
// share it, their calls interleaving.
type drawList struct {
	// outsideFrame is the draw list's report of an entry recorded or a camera
	// set outside the frame, and slicesDiffer of a bulk call given parallel
	// slices of different lengths, each made once; its message is the key
	// it is logged once under.
	outsideFrame error
	slicesDiffer error
	// mutex guards entries, shapes, sprites, arguments and cameras.
	mutex     sync.Mutex
	entries   []recordedEntry
	shapes    []Shape
	sprites   []Sprite
	arguments []byte
	cameras   []Camera
}

// record appends recorded to records, one of the draw list's slices, under
// its lock, and an entry of kind for each to its entries, kind zero for
// cameras, which are no entries, each giving its material argument, of
// argumentType, nil for none, its bytes in turn of argumentBytes, unless
// state has no frame open for entries, which it reports once for the draw
// list, outside the lock.
func (l *drawList) record[T any](state *drawState, kind EntryKind, records *[]T, argumentType reflect.Type, argumentBytes []byte, recorded ...T) {
	l.mutex.Lock()
	recording := state.recording.Load()
	if recording {
		if kind != 0 {
			argumentSize := findArgumentSize(argumentType)
			for index := range recorded {
				argument := recordedArgument{goType: argumentType, offset: uint32(len(l.arguments)) + uint32(index)*argumentSize}
				l.entries = append(l.entries, recordedEntry{kind: kind, index: uint32(len(*records) + index), argument: argument})
			}
			l.arguments = append(l.arguments, argumentBytes...)
		}
		*records = append(*records, recorded...)
	}
	l.mutex.Unlock()
	if !recording {
		state.logger.LogErrOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// moveEntries appends the draw list's entries, shapes, sprites, arguments
// and cameras to state's, each entry's index moved past state's records of
// its kind, and its argument's offset past state's arguments, and empties
// it.
func (l *drawList) moveEntries(state *drawState) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	// recordsBefore are state's records of each kind, at the kind.
	recordsBefore := [...]uint32{EntryKindShape: uint32(len(state.shapes)), EntryKindSprite: uint32(len(state.sprites))}
	argumentsBefore := uint32(len(state.arguments))
	for _, entry := range l.entries {
		entry.index += recordsBefore[entry.kind]
		entry.argument.offset += argumentsBefore
		state.entries = append(state.entries, entry)
	}
	state.shapes = append(state.shapes, l.shapes...)
	state.sprites = append(state.sprites, l.sprites...)
	state.arguments = append(state.arguments, l.arguments...)
	state.cameras = append(state.cameras, l.cameras...)
	l.entries = l.entries[:0]
	l.shapes = l.shapes[:0]
	l.sprites = l.sprites[:0]
	l.arguments = l.arguments[:0]
	l.cameras = l.cameras[:0]
}

// recordedEntry is one entry of a draw list, or of the frame as merged: its
// kind, its place among the records of that kind beside it, and the
// material argument it gives.
type recordedEntry struct {
	kind     EntryKind
	index    uint32
	argument recordedArgument
}

// recordedArgument is the material argument an entry gives, as recorded:
// its Go type, nil for none, and where its bytes begin among the arguments
// beside it, its draw list's or the frame's.
type recordedArgument struct {
	goType reflect.Type
	offset uint32
}
