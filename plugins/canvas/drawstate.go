package canvas

import (
	"fmt"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
)

// drawState is canvas's private state: the lane of each task declaring a
// DrawList, in registration order, whether a frame is open for entries, the
// entries of the frame last merged from the lanes, and the inspector.
type drawState struct {
	// logger is the kernel's, where reports go.
	logger *kernel.Logger
	// recording is set from the job opening a frame for entries until the
	// job merging its lanes.
	recording atomic.Bool
	// lanes are added at registration, while the kernel is composed, and
	// only read after.
	lanes []*lane
	// shapes are the frame's shapes, merged from the lanes in their order,
	// and replaced by the next frame's when it merges.
	shapes []Shape
	// inspector keeps the inspection Assets.RequestInspection asks for.
	inspector inspector
	// frameInspected is set when the frame merged is written down for a
	// pending request, and then laneEnds holds, for each lane, the index in
	// shapes after its last shape.
	frameInspected bool
	laneEnds       []int
}

// addLane adds a lane after every lane added before it, and answers it.
func (s *drawState) addLane() *lane {
	added := &lane{outsideFrame: Report{
		Kind: ReportKindEntryOutsideFrame,
		Err:  fmt.Errorf("canvas: draw list %d recorded an entry while no frame is recording", len(s.lanes)),
	}}
	s.lanes = append(s.lanes, added)
	return added
}

// closeAndMerge closes the frame to entries, and replaces the shapes of the
// frame merged before with each lane's, lane after lane, emptying the lanes.
// The frame is inspected when a request is pending as it merges, and then
// where each lane's shapes end is kept.
func (s *drawState) closeAndMerge() {
	s.recording.Store(false)
	s.frameInspected = s.inspector.requestPending.Load()
	s.shapes = s.shapes[:0]
	s.laneEnds = s.laneEnds[:0]
	for _, taskLane := range s.lanes {
		s.shapes = taskLane.moveShapes(s.shapes)
		if s.frameInspected {
			s.laneEnds = append(s.laneEnds, len(s.shapes))
		}
	}
}

// makeInspection answers the frame merged, of beat, in canvas's terms: no
// camera when nothing was drawn, and otherwise the default camera into
// window, of one batch of every shape, each with its place among the lanes.
// It is called only for a frame inspected, whose lane ends were kept.
func (s *drawState) makeInspection(beat metronome.Beat, window platform.Window, cameraDrawn bool) Inspection {
	if !s.frameInspected {
		panic("canvas: making the inspection of a frame not inspected, whose lane ends were not kept")
	}
	inspection := Inspection{Beat: beat}
	if !cameraDrawn {
		return inspection
	}
	entries := make([]InspectedEntry, 0, len(s.shapes))
	laneStart := 0
	for laneIndex, laneEnd := range s.laneEnds {
		for index, shape := range s.shapes[laneStart:laneEnd] {
			entries = append(entries, InspectedEntry{
				Kind:            EntryKindShape,
				Shape:           shape,
				Layer:           shape.Layer,
				Order:           shape.Order,
				DrawListIndex:   laneIndex,
				IndexInDrawList: index,
			})
		}
		laneStart = laneEnd
	}
	inspection.Cameras = []InspectedCamera{{
		Order:   defaultCameraOrder,
		Window:  window,
		Batches: []InspectedBatch{{InstanceCount: uint32(len(entries)), Entries: entries}},
	}}
	return inspection
}
