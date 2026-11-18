package canvas

import (
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
	l.list.record(l.state.Get(), &l.list.shapes, shape)
}

// SetCamera sets camera for the frame being recorded, forgotten once the
// frame is drawn, as its entries are: nothing is made or kept. A frame that
// sets no camera is seen through the default one, which draws only a frame
// with entries: every layer, into the whole main window, cleared to opaque
// black, at order 0. Outside the frame, it is refused and reported once for
// the task.
func (l DrawList) SetCamera(camera Camera) {
	l.list.record(l.state.Get(), &l.list.cameras, camera)
}

// drawList is one task's draw list for the frame being recorded, which its
// DrawList records into: the entries its jobs record and the cameras they
// set, each in the order recorded. Two jobs of one task running together
// share it, their calls interleaving.
type drawList struct {
	// outsideFrame is the draw list's report of an entry recorded or a camera
	// set outside the frame, made once; its message is the key it is
	// logged once under.
	outsideFrame error
	// mutex guards shapes and cameras.
	mutex   sync.Mutex
	shapes  []Shape
	cameras []Camera
}

// record appends value to values, one of the draw list's slices, under its
// lock, unless state has no frame open for entries, which it reports once
// for the draw list, outside the lock.
func (l *drawList) record[T any](state *drawState, values *[]T, value T) {
	l.mutex.Lock()
	recording := state.recording.Load()
	if recording {
		*values = append(*values, value)
	}
	l.mutex.Unlock()
	if !recording {
		state.logger.LogErrOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// moveEntries appends the draw list's shapes to shapes and its cameras to
// cameras, empties it, and answers the two appended to.
func (l *drawList) moveEntries(shapes []Shape, cameras []Camera) ([]Shape, []Camera) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	shapes = append(shapes, l.shapes...)
	cameras = append(cameras, l.cameras...)
	l.shapes = l.shapes[:0]
	l.cameras = l.cameras[:0]
	return shapes, cameras
}
