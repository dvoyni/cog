package canvas

import (
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
)

// drawState is canvas's private state: the lane of each task declaring a
// DrawList, in registration order, whether a frame is open for entries, the
// entries of the frame last sorted, merged from the lanes, and the
// inspector.
type drawState struct {
	// logger is the kernel's, where reports go.
	logger *kernel.Logger
	// recording is set from the job opening a frame for entries until the
	// job sorting them.
	recording atomic.Bool
	// lanes are added at registration, while the kernel is composed, and
	// only read after.
	lanes []*lane
	// shapes are the frame's shapes, merged from the lanes in their order,
	// and replaced by the next frame's when it merges.
	shapes []Shape
	// sorter sorts them, and holds the frame's entries in sort order.
	sorter entrySorter
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

// closeAndSort closes the frame to entries, replaces the shapes of the
// frame merged before with each lane's, lane after lane, emptying the lanes,
// and sorts them. The frame is inspected when a request is pending as it
// merges, and then where each lane's shapes end is kept.
func (s *drawState) closeAndSort() {
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
	s.sorter.sort(s.shapes)
}

// makeInspection answers the frame sorted, of beat, in canvas's terms: no
// camera when nothing was drawn, and otherwise the default camera into
// window, of batches, the batches it drew of the frame's entries, each
// entry with its place among the lanes. It is called only for a frame
// inspected, whose lane ends were kept.
func (s *drawState) makeInspection(beat metronome.Beat, window platform.Window, cameraDrawn bool, batches []batch) Inspection {
	if !s.frameInspected {
		panic("canvas: making the inspection of a frame not inspected, whose lane ends were not kept")
	}
	inspection := Inspection{Beat: beat}
	if !cameraDrawn {
		return inspection
	}
	inspectedBatches := make([]InspectedBatch, 0, len(batches))
	for _, drawnBatch := range batches {
		inspectedEntries := make([]InspectedEntry, 0, len(drawnBatch.entries))
		for _, entry := range drawnBatch.entries {
			shapeIndex := int(entry.shapeIndex)
			// The lane holding the shape is the first to end after it.
			laneIndex, _ := slices.BinarySearch(s.laneEnds, shapeIndex+1)
			laneStart := 0
			if laneIndex > 0 {
				laneStart = s.laneEnds[laneIndex-1]
			}
			shape := s.shapes[shapeIndex]
			inspectedEntries = append(inspectedEntries, InspectedEntry{
				Kind:            EntryKindShape,
				Shape:           shape,
				Layer:           shape.Layer,
				Order:           shape.Order,
				DrawListIndex:   laneIndex,
				IndexInDrawList: shapeIndex - laneStart,
			})
		}
		inspectedBatches = append(inspectedBatches, InspectedBatch{Material: drawnBatch.key.material, InstanceCount: drawnBatch.instanceCount, Entries: inspectedEntries})
	}
	inspection.Cameras = []InspectedCamera{{
		Order:   defaultCameraOrder,
		Window:  window,
		Batches: inspectedBatches,
	}}
	return inspection
}
