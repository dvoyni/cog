package render

import (
	"cmp"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/plugins/metronome"
)

// inspector keeps the inspection Frame.RequestInspection asks for, one
// recording at a time, from its request to its delivery: requested until
// the next frame begins, and recording while that frame records, render's
// calls appending to it. At the frame's end it leaves the recording, so a
// request then asks for the next frame to begin: it waits for the frame's
// pass timings when the frame was timed, and is then delivered, until
// InspectedEvent's jobs have run, each kept by its frame's beat. Those jobs
// are published in a batch of their own, which holds back no request. The
// frame state and the asset table share it, through their reporters.
type inspector struct {
	// phase is where the inspection recording stands, read without the
	// mutex, so every call of render's checks it, the one cost a frame not
	// inspected pays, and changed under it.
	phase atomic.Uint32
	// mutex guards the rest: the inspection recording, its command lists
	// with their orders, sorted by them at the frame's end, and whether its
	// frame is timed; the inspections of frames ended that wait for their
	// pass timings; and those delivered, whose InspectedEvent's jobs run.
	mutex                sync.Mutex
	inspection           Inspection
	lists                []orderedInspectedList
	passTimingsAwaited   bool
	timedInspections     []Inspection
	deliveredInspections []Inspection
}

// isRecording answers whether a frame being inspected is recording, so a
// call appends to its inspection.
func (i *inspector) isRecording() bool {
	return inspectionPhase(i.phase.Load()) == inspectionPhaseRecording
}

// request asks for an inspection of the next frame to begin; a request
// while one is requested or recording joins it.
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
// sorted by order, and delivers it, answering its beat; false when none is
// recording, and when it waits for the frame's pass timings, when
// addPassTimings delivers it. Either way no inspection is recording after
// it, so the next request asks for another.
func (i *inspector) finish() (metronome.Beat, bool) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if !i.isRecording() {
		return metronome.Beat{}, false
	}
	slices.SortStableFunc(i.lists, func(a, b orderedInspectedList) int { return cmp.Compare(a.order, b.order) })
	for _, list := range i.lists {
		i.inspection.CommandLists = append(i.inspection.CommandLists, *list.list)
	}
	i.lists = nil
	ended := i.inspection
	i.inspection = Inspection{}
	i.phase.Store(uint32(inspectionPhaseIdle))
	if i.passTimingsAwaited {
		i.timedInspections = append(i.timedInspections, ended)
		return metronome.Beat{}, false
	}
	i.deliveredInspections = append(i.deliveredInspections, ended)
	return ended.Beat, true
}

// addPassTimings writes timings down in the inspection waiting for the pass
// timings of the frame of beat, and delivers it, answering true; nil
// timings, of a frame whose timings were not read back, deliver it without.
// False when no inspection waits for them.
func (i *inspector) addPassTimings(beat metronome.Beat, timings []PassTiming) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	index := slices.IndexFunc(i.timedInspections, func(timed Inspection) bool { return timed.Beat == beat })
	if index < 0 {
		return false
	}
	timed := i.timedInspections[index]
	i.timedInspections = slices.Delete(i.timedInspections, index, index+1)
	timed.PassTimings = slices.Clone(timings)
	i.deliveredInspections = append(i.deliveredInspections, timed)
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

// drop drops the inspection of the frame of beat, once InspectedEvent's
// jobs have run.
func (i *inspector) drop(beat metronome.Beat) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.deliveredInspections = slices.DeleteFunc(i.deliveredInspections, func(delivered Inspection) bool { return delivered.Beat == beat })
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

// inspectionPhase is where an inspector's inspection recording stands.
type inspectionPhase uint32

const (
	// inspectionPhaseIdle is no inspection asked for.
	inspectionPhaseIdle inspectionPhase = iota
	// inspectionPhaseRequested is one asked for, its frame not yet begun.
	inspectionPhaseRequested
	// inspectionPhaseRecording is its frame recording.
	inspectionPhaseRecording
)

// orderedInspectedList is an inspected command list with the order its list
// was begun at.
type orderedInspectedList struct {
	order Order
	list  *InspectedCommandList
}
