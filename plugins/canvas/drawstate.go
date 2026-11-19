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
// draw lists, with each entry's keys, and the inspector.
type drawState struct {
	// logger is the kernel's, where reports go.
	logger *kernel.Logger
	// defaultFilter is the filter of an entry naming none,
	// Config.DefaultFilter's, never FilterDefault.
	defaultFilter Filter
	// recording is set from the job opening a frame for entries until the
	// job sorting them.
	recording atomic.Bool
	// drawLists are added at registration, while the kernel is composed,
	// and only read after.
	drawLists []*drawList
	// entries are the frame's entries, merged from the draw lists in their
	// order, each a shape of shapes or a sprite of sprites, merged alike.
	// All are replaced by the next frame's when it merges.
	entries []recordedEntry
	shapes  []Shape
	sprites []Sprite
	// cameras are the frame's cameras, merged from the draw lists in their
	// order, in the order they draw, or the default camera for a frame of
	// entries that set none, replaced by the next frame's when it merges.
	cameras []Camera
	// sorter sorts them, and holds the frame's entries in sort order, which
	// the renderer regroups once it has found their batch keys.
	sorter entrySorter
	// inspector keeps the inspection Assets.RequestInspection asks for.
	inspector inspector
	// frameInspected is set when the frame merged is written down for a
	// pending request, and then drawListEnds holds, for each draw list, the
	// index in entries after its last entry.
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

// closeAndSort closes the frame to entries, replaces the entries and the
// cameras of the frame merged before with each draw list's, list after
// list, emptying them, and sorts both: the entries by sort key, to be
// regrouped by batch where their layer asks, and the
// cameras in the order they draw, the default camera standing in when the
// frame recorded entries and set no camera. The frame is inspected when a
// request is pending as it merges, and then where each draw list's entries
// end is kept.
func (s *drawState) closeAndSort() {
	s.recording.Store(false)
	s.frameInspected = s.inspector.requestPending.Load()
	s.entries = s.entries[:0]
	s.shapes = s.shapes[:0]
	s.sprites = s.sprites[:0]
	s.cameras = s.cameras[:0]
	s.drawListEnds = s.drawListEnds[:0]
	for _, taskDrawList := range s.drawLists {
		taskDrawList.moveEntries(s)
		if s.frameInspected {
			s.drawListEnds = append(s.drawListEnds, len(s.entries))
		}
	}
	if len(s.cameras) == 0 && len(s.entries) > 0 {
		s.cameras = append(s.cameras, defaultCamera)
	}
	slices.SortStableFunc(s.cameras, compareCameras)
	s.findSortKeys()
	s.sorter.sortByKey()
}

// findSortKeys writes over the sorter's entries each entry with its sort
// key, in the order merged, for the sorter to sort.
func (s *drawState) findSortKeys() {
	s.sorter.entries = s.sorter.entries[:0]
	for index, entry := range s.entries {
		var key sortKey
		if entry.kind == EntryKindShape {
			shape := &s.shapes[entry.index]
			key = makeSortKey(shape.Layer, shape.Order)
		} else {
			sprite := &s.sprites[entry.index]
			key = makeSortKey(sprite.Layer, sprite.Order)
		}
		s.sorter.entries = append(s.sorter.entries, sortedEntry{key: key, entryIndex: uint32(index)})
	}
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
				entryIndex := int(entry.entryIndex)
				// The draw list holding the entry is the first to end after
				// it.
				drawListIndex, _ := slices.BinarySearch(s.drawListEnds, entryIndex+1)
				drawListStart := 0
				if drawListIndex > 0 {
					drawListStart = s.drawListEnds[drawListIndex-1]
				}
				recorded := s.entries[entryIndex]
				inspectedEntry := InspectedEntry{Kind: recorded.kind, DrawListIndex: drawListIndex, IndexInDrawList: entryIndex - drawListStart}
				if recorded.kind == EntryKindShape {
					inspectedEntry.Shape = s.shapes[recorded.index]
					inspectedEntry.Layer = inspectedEntry.Shape.Layer
					inspectedEntry.Order = inspectedEntry.Shape.Order
				} else {
					inspectedEntry.Sprite = s.sprites[recorded.index]
					inspectedEntry.Layer = inspectedEntry.Sprite.Layer
					inspectedEntry.Order = inspectedEntry.Sprite.Order
				}
				inspectedEntries = append(inspectedEntries, inspectedEntry)
			}
			key := drawnBatch.key
			inspectedBatches = append(inspectedBatches, InspectedBatch{Material: key.material, Atlas: key.atlas, Filter: key.filter, Clip: key.clip, InstanceCount: drawnBatch.instanceCount, Entries: inspectedEntries})
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
