package canvas

import "github.com/dvoyni/cog/kernel"

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
// An entry is recorded only from FrameEvent's Ready until canvas's drawing
// begins; one recorded at any other time is refused, and reported once for
// the task, as ReportKindEntryOutsideFrame.
type DrawList struct {
	state kernel.Read[*drawState]
	lane  *lane
}

// Declare declares DrawList's lock, gives the task being registered a draw
// list of its own, after every list given before, and answers DrawList
// bound to it.
func (DrawList) Declare(access *kernel.Access) DrawList {
	state := access.Read[*drawState]()
	return DrawList{state: state, lane: state.Get().addLane()}
}

// DrawShape records shape into the task's draw list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l DrawList) DrawShape(shape Shape) {
	l.lane.recordShape(l.state.Get(), shape)
}
