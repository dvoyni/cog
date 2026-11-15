package render

import (
	"cmp"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/metronome"
)

// inspector keeps the inspection Frame.RequestInspection asks for, one at a
// time, from its request to its delivery: requested until the next frame
// begins, recording while that frame records, render's calls appending to
// it, waiting for the frame's pass timings when the frame was timed, and
// delivered from the frame's end, or from its timings, until
// InspectedEvent's jobs have run. The frame state and the asset table share
// it, through their reporters.
type inspector struct {
	// phase is where the inspection stands, read without the mutex, so
	// every call of render's checks it, the one cost a frame not inspected
	// pays, and changed under it.
	phase atomic.Uint32
	// mutex guards the rest: the inspection, and its command lists with
	// their orders, sorted by them at the frame's end.
	mutex      sync.Mutex
	inspection Inspection
	lists      []orderedInspectedList
	// passTimingsAwaited is set while the frame recording is timed, its
	// pass timings read back after its end.
	passTimingsAwaited bool
}

// isRecording answers whether a frame being inspected is recording, so a
// call appends to its inspection.
func (i *inspector) isRecording() bool {
	return inspectionPhase(i.phase.Load()) == inspectionPhaseRecording
}

// request asks for an inspection of the next frame to begin; a request
// while one is pending joins it.
func (i *inspector) request() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.phase.CompareAndSwap(uint32(inspectionPhaseIdle), uint32(inspectionPhaseRequested))
}

// begin begins the inspection of the frame of beat, when one is requested.
func (i *inspector) begin(beat metronome.Beat) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if inspectionPhase(i.phase.Load()) != inspectionPhaseRequested {
		return
	}
	i.inspection = Inspection{Beat: beat}
	i.passTimingsAwaited = false
	i.phase.Store(uint32(inspectionPhaseRecording))
}

// awaitPassTimings has the inspection of the frame recording, if any, wait
// for the frame's pass timings, which are read back after its end.
func (i *inspector) awaitPassTimings() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.passTimingsAwaited = i.isRecording()
}

// finish ends the inspection of the frame recording, its command lists
// sorted by order, and answers its beat, to deliver; false when none is
// recording, and when it awaits the frame's pass timings, when
// addPassTimings delivers it.
func (i *inspector) finish() (metronome.Beat, bool) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if !i.isRecording() {
		return metronome.Beat{}, false
	}
	if i.passTimingsAwaited {
		i.phase.Store(uint32(inspectionPhaseTiming))
	} else {
		i.phase.Store(uint32(inspectionPhaseDelivered))
	}
	slices.SortStableFunc(i.lists, func(a, b orderedInspectedList) int { return cmp.Compare(a.order, b.order) })
	for _, list := range i.lists {
		i.inspection.CommandLists = append(i.inspection.CommandLists, *list.list)
	}
	i.lists = nil
	return i.inspection.Beat, !i.passTimingsAwaited
}

// addPassTimings writes timings down in the inspection waiting for the pass
// timings of the frame of beat, and answers true, for it to be delivered;
// nil timings, of a frame whose timings were not read back, deliver it
// without. False when no inspection waits for them.
func (i *inspector) addPassTimings(beat metronome.Beat, timings []PassTiming) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if inspectionPhase(i.phase.Load()) != inspectionPhaseTiming || i.inspection.Beat != beat {
		return false
	}
	i.inspection.PassTimings = slices.Clone(timings)
	i.phase.Store(uint32(inspectionPhaseDelivered))
	return true
}

// read answers the inspection of the frame of beat while it is delivered.
func (i *inspector) read(beat metronome.Beat) (Inspection, bool) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if inspectionPhase(i.phase.Load()) != inspectionPhaseDelivered || i.inspection.Beat != beat {
		return Inspection{}, false
	}
	return i.inspection, true
}

// drop drops the delivered inspection, once InspectedEvent's jobs have
// run, so the next request asks for another.
func (i *inspector) drop() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.inspection = Inspection{}
	i.phase.Store(uint32(inspectionPhaseIdle))
}

// addList answers the inspected list of a command list labelled label,
// begun at order, for its own task to fill; nil when no frame is being
// inspected.
func (i *inspector) addList(order Order, label string) *InspectedCommandList {
	if !i.isRecording() {
		return nil
	}
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if !i.isRecording() {
		return nil
	}
	list := &InspectedCommandList{Label: label}
	i.lists = append(i.lists, orderedInspectedList{order: order, list: list})
	return list
}

// add hands the inspection being recorded to change, under i's mutex;
// nothing when no frame is being inspected, which a check inlined into its
// caller finds.
func (i *inspector) add(change func(inspection *Inspection)) {
	if i.isRecording() {
		i.addLocked(change)
	}
}

// addLocked hands the inspection being recorded to change under i's mutex,
// unless it ended since the check before.
func (i *inspector) addLocked(change func(inspection *Inspection)) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if i.isRecording() {
		change(&i.inspection)
	}
}

// inspectionPhase is where an inspector's inspection stands.
type inspectionPhase uint32

const (
	// inspectionPhaseIdle is no inspection asked for.
	inspectionPhaseIdle inspectionPhase = iota
	// inspectionPhaseRequested is one asked for, its frame not yet begun.
	inspectionPhaseRequested
	// inspectionPhaseRecording is its frame recording.
	inspectionPhaseRecording
	// inspectionPhaseTiming is its frame ended, its pass timings being
	// read back.
	inspectionPhaseTiming
	// inspectionPhaseDelivered is its frame ended, until InspectedEvent's
	// jobs have run.
	inspectionPhaseDelivered
)

// orderedInspectedList is an inspected command list with the order its list
// was begun at.
type orderedInspectedList struct {
	order Order
	list  *InspectedCommandList
}
