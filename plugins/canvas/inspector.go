package canvas

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render"
)

// inspector keeps the inspection Assets.RequestInspection asks for, from its
// request to its delivery. While a request is pending, each frame canvas
// draws is written down; render's InspectedEvent then names the frame render
// inspected, and the one canvas wrote down of it is delivered, render's
// beside it, until canvas's InspectedEvent's jobs have run. Writing down
// every frame of a pending request, rather than one chosen at the request,
// keeps the two accounts of one frame even when render joins canvas's
// request to an inspection already pending, or when the request lands while
// a frame begins.
type inspector struct {
	// requestPending is set from a request until its delivery, a request while
	// one is pending joining it, and read without the mutex, the one check a
	// frame not inspected pays.
	requestPending atomic.Bool
	// mutex guards the rest: recordedFrames, the frames written down while
	// the request is pending, oldest first; and deliveredInspections, the
	// inspections whose InspectedEvent's jobs run.
	mutex                sync.Mutex
	recordedFrames       []Inspection
	deliveredInspections []Inspection
}

// record writes inspection down, the frame drawn while a request was
// pending, unless that request was delivered since.
func (i *inspector) record(inspection Inspection) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if i.requestPending.Load() {
		i.recordedFrames = append(i.recordedFrames, inspection)
	}
}

// deliver delivers the frame of beat, which render inspected, with
// renderInspection beside it, and answers true, when it was written down:
// the request is then answered, and the other frames written down for it
// forgotten. Otherwise it forgets the frames up to beat, which render will
// not inspect now, and answers false.
func (i *inspector) deliver(beat metronome.Beat, renderInspection render.Inspection) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	index := slices.IndexFunc(i.recordedFrames, func(recorded Inspection) bool { return recorded.Beat == beat })
	if index < 0 {
		i.recordedFrames = slices.DeleteFunc(i.recordedFrames, func(recorded Inspection) bool { return recorded.Beat.Tick <= beat.Tick })
		return false
	}
	inspection := i.recordedFrames[index]
	inspection.Render = renderInspection
	i.deliveredInspections = append(i.deliveredInspections, inspection)
	clear(i.recordedFrames)
	i.recordedFrames = i.recordedFrames[:0]
	i.requestPending.Store(false)
	return true
}

// read answers the inspection of the frame of beat while it is delivered.
func (i *inspector) read(beat metronome.Beat) (Inspection, bool) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	index := slices.IndexFunc(i.deliveredInspections, func(delivered Inspection) bool { return delivered.Beat == beat })
	if index < 0 {
		return Inspection{}, false
	}
	return i.deliveredInspections[index], true
}

// drop drops the inspection of the frame of beat, once its InspectedEvent's
// jobs have run.
func (i *inspector) drop(beat metronome.Beat) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.deliveredInspections = slices.DeleteFunc(i.deliveredInspections, func(delivered Inspection) bool { return delivered.Beat == beat })
}
