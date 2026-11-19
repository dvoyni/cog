package canvas

import (
	"fmt"
	"sync"

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
	l.list.record(l.state.Get(), EntryKindShape, &l.list.shapes, shape)
}

// DrawSprite records sprite into the task's draw list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l DrawList) DrawSprite(sprite Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, sprite)
}

// DrawSprites records sprites into the task's draw list for the frame being
// recorded, in their order, as DrawSprite would one after another, so an
// ECS chunk's column goes in as it is. Outside the frame, they are refused
// and reported once for the task.
func (l DrawList) DrawSprites(sprites []Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, sprites...)
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
	l.list.record(l.state.Get(), 0, &l.list.cameras, camera)
}

// drawList is one task's draw list for the frame being recorded, which its
// DrawList records into: the entries its jobs record, in the order
// recorded, each of them a shape or a sprite of its records of that kind,
// and the cameras they set, in the order set. Two jobs of one task running
// together share it, their calls interleaving.
type drawList struct {
	// outsideFrame is the draw list's report of an entry recorded or a camera
	// set outside the frame, made once; its message is the key it is
	// logged once under.
	outsideFrame error
	// mutex guards entries, shapes, sprites and cameras.
	mutex   sync.Mutex
	entries []recordedEntry
	shapes  []Shape
	sprites []Sprite
	cameras []Camera
}

// record appends recorded to records, one of the draw list's slices, under
// its lock, and an entry of kind for each to its entries, kind zero for
// cameras, which are no entries, unless state has no frame open for
// entries, which it reports once for the draw list, outside the lock.
func (l *drawList) record[T any](state *drawState, kind EntryKind, records *[]T, recorded ...T) {
	l.mutex.Lock()
	recording := state.recording.Load()
	if recording {
		if kind != 0 {
			for index := range recorded {
				l.entries = append(l.entries, recordedEntry{kind: kind, index: uint32(len(*records) + index)})
			}
		}
		*records = append(*records, recorded...)
	}
	l.mutex.Unlock()
	if !recording {
		state.logger.LogErrOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// moveEntries appends the draw list's entries, shapes, sprites and cameras
// to state's, each entry's index moved past state's records of its kind,
// and empties it.
func (l *drawList) moveEntries(state *drawState) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	// recordsBefore are state's records of each kind, at the kind.
	recordsBefore := [...]uint32{EntryKindShape: uint32(len(state.shapes)), EntryKindSprite: uint32(len(state.sprites))}
	for _, entry := range l.entries {
		entry.index += recordsBefore[entry.kind]
		state.entries = append(state.entries, entry)
	}
	state.shapes = append(state.shapes, l.shapes...)
	state.sprites = append(state.sprites, l.sprites...)
	state.cameras = append(state.cameras, l.cameras...)
	l.entries = l.entries[:0]
	l.shapes = l.shapes[:0]
	l.sprites = l.sprites[:0]
	l.cameras = l.cameras[:0]
}

// recordedEntry is one entry of a draw list, or of the frame as merged: its
// kind, and its place among the records of that kind beside it.
type recordedEntry struct {
	kind  EntryKind
	index uint32
}
