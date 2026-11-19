package canvas

import (
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
)

// drawState is canvas's private state: the draw list of each task
// declaring a DrawList, in registration order, whether a frame is open for
// entries, the entries and cameras of the frame last sorted, merged from the
// draw lists, and the inspector.
type drawState struct {
	// logger is the kernel's, where reports go.
	logger *kernel.Logger
	// recording is set from the job opening a frame for entries until the
	// job sorting them.
	recording atomic.Bool
	// drawLists are added at registration, while the kernel is composed,
	// and only read after.
	drawLists []*drawList
	// shapes are the frame's shapes, merged from the draw lists in their order,
	// and replaced by the next frame's when it merges.
	shapes []Shape
	// cameras are the frame's cameras, merged from the draw lists in their
	// order, in the order they draw, or the default camera for a frame of
	// entries that set none, replaced by the next frame's when it merges.
	cameras []Camera
	// sorter sorts them, and holds the frame's entries in sort order.
	sorter entrySorter
	// inspector keeps the inspection Assets.RequestInspection asks for.
	inspector inspector
	// frameInspected is set when the frame merged is written down for a
	// pending request, and then drawListEnds holds, for each draw list, the
	// index in shapes after its last shape.
	frameInspected bool
	drawListEnds   []int
}

// addDrawList adds a draw list after every one added before it, and
// answers it.
func (s *drawState) addDrawList() *drawList {
	added := &drawList{outsideFrame: Report{
		Kind: ReportKindRecordedOutsideFrame,
		Err:  fmt.Errorf("canvas: draw list %d recorded an entry or set a camera while no frame is recording", len(s.drawLists)),
	}}
	s.drawLists = append(s.drawLists, added)
	return added
}

// closeAndSort closes the frame to entries, replaces the shapes and the
// cameras of the frame merged before with each draw list's, list after
// list, emptying them, and sorts both: the shapes by sort key, and the
// cameras in the order they draw, the default camera standing in when the
// frame recorded shapes and set no camera. The frame is inspected when a
// request is pending as it merges, and then where each draw list's shapes end is
// kept.
func (s *drawState) closeAndSort() {
	s.recording.Store(false)
	s.frameInspected = s.inspector.requestPending.Load()
	s.shapes = s.shapes[:0]
	s.cameras = s.cameras[:0]
	s.drawListEnds = s.drawListEnds[:0]
	for _, taskDrawList := range s.drawLists {
		s.shapes, s.cameras = taskDrawList.moveEntries(s.shapes, s.cameras)
		if s.frameInspected {
			s.drawListEnds = append(s.drawListEnds, len(s.shapes))
		}
	}
	if len(s.cameras) == 0 && len(s.shapes) > 0 {
		s.cameras = append(s.cameras, defaultCamera)
	}
	slices.SortStableFunc(s.cameras, compareCameras)
	s.sorter.sort(s.shapes)
}

// makeInspection answers the frame sorted, of beat, in canvas's terms: each
// camera of cameraDraws, the cameras that drew, in the order they drew, with
// the batches it drew, each entry with its place among the draw lists. It is
// called only for a frame inspected, whose draw lists' ends were kept.
func (s *drawState) makeInspection(beat metronome.Beat, cameraDraws []cameraDraw) Inspection {
	if !s.frameInspected {
		panic("canvas: making the inspection of a frame not inspected, whose draw lists' ends were not kept")
	}
	inspection := Inspection{Beat: beat}
	for _, drawnCamera := range cameraDraws {
		var inspectedBatches []InspectedBatch
		for _, drawnBatch := range drawnCamera.batches {
			inspectedEntries := make([]InspectedEntry, 0, len(drawnBatch.entries))
			for _, entry := range drawnBatch.entries {
				shapeIndex := int(entry.shapeIndex)
				// The draw list holding the shape is the first to end after
				// it.
				drawListIndex, _ := slices.BinarySearch(s.drawListEnds, shapeIndex+1)
				drawListStart := 0
				if drawListIndex > 0 {
					drawListStart = s.drawListEnds[drawListIndex-1]
				}
				shape := s.shapes[shapeIndex]
				inspectedEntries = append(inspectedEntries, InspectedEntry{
					Kind:            EntryKindShape,
					Shape:           shape,
					Layer:           shape.Layer,
					Order:           shape.Order,
					DrawListIndex:   drawListIndex,
					IndexInDrawList: shapeIndex - drawListStart,
				})
			}
			inspectedBatches = append(inspectedBatches, InspectedBatch{Material: drawnBatch.key.material, InstanceCount: drawnBatch.instanceCount, Entries: inspectedEntries})
		}
		inspection.Cameras = append(inspection.Cameras, InspectedCamera{
			Camera:   drawnCamera.camera,
			Window:   drawnCamera.target.window,
			Viewport: drawnCamera.view.viewport,
			Batches:  inspectedBatches,
		})
	}
	return inspection
}
