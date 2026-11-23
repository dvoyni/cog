package canvas

import (
	"cmp"
	"fmt"
	"slices"
	"sync/atomic"

	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render"
)

// drawState is canvas's private state: the draw list of each task
// declaring a DrawList, in registration order, whether a frame is open for
// entries, the entries and cameras of the frame last sorted, merged from the
// draw lists, with each entry's keys, the inspector, the residency requests
// Assets queued, the image sizes it read, the 2D materials it made, and the
// stopwatches timing its jobs.
type drawState struct {
	// reporter is where reports go.
	reporter *reporter
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
	// order, each a shape of shapes, a sprite of sprites, a run of text of
	// texts, per-frame triangles of triangles or a mesh of meshes, merged
	// alike, the bytes of the arguments they give, end to end, the spans of
	// the runs of text, copied end to end, and the triangles' vertex and
	// index bytes, each end to end. All are replaced by the next frame's
	// when it merges.
	entries     []recordedEntry
	shapes      []Shape
	sprites     []Sprite
	texts       []laidOutText
	triangles   []recordedTriangles
	meshes      []Mesh
	arguments   []byte
	spans       []Span
	vertexBytes []byte
	indexBytes  []byte
	// cameras are the frame's cameras, merged from the draw lists in their
	// order, in the order they draw, or the default camera for a frame of
	// entries that set none, replaced by the next frame's when it merges.
	cameras []Camera
	// sorter sorts them, and holds the frame's entries in sort order, which
	// the renderer regroups once it has found their batch keys.
	sorter entrySorter
	// inspector keeps the inspection Assets.RequestInspection asks for,
	// shared with reporter and the FontLibrary, which note what goes into
	// it.
	inspector *inspector
	// residency queues Assets.Preload's and Assets.Unload's requests for
	// the renderer, and imageSizes keeps Assets.ImageSize's answers.
	residency  residencyRequestQueue
	imageSizes imageSizeTable
	// materials are the 2D materials Assets made.
	materials materialTable
	// frameInspected is set when the frame merged is written down for a
	// pending request, and then drawListEnds holds, for each draw list, the
	// index in entries after its last entry.
	frameInspected bool
	drawListEnds   []int
	// sortStopwatch and drawStopwatch time the jobs sorting and drawing a
	// frame inspected, and sortTime is how long it took to sort.
	sortStopwatch metronome.Stopwatch
	drawStopwatch metronome.Stopwatch
	sortTime      time.Duration
}

// addDrawList adds a draw list after every one added before it, and
// answers it.
func (s *drawState) addDrawList() *drawList {
	added := &drawList{
		outsideFrame: Report{
			Kind: ReportKindRecordedOutsideFrame,
			Err:  fmt.Errorf("canvas: draw list %d recorded an entry or set a camera while no frame is recording", len(s.drawLists)),
		},
		slicesDiffer: Report{
			Kind: ReportKindSliceLengthsDiffer,
			Err:  fmt.Errorf("canvas: draw list %d was given parallel slices of different lengths, of entries and their arguments, and recorded none of them", len(s.drawLists)),
		},
		boundsMissing: Report{
			Kind: ReportKindBoundsMissing,
			Err:  fmt.Errorf("canvas: draw list %d was given triangles of zero bounds, and recorded none of them: a mesh needs its bounds given, or it would vanish as its origin left a view", len(s.drawLists)),
		},
	}
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
	if s.frameInspected {
		s.sortStopwatch.Start()
	}
	s.entries = s.entries[:0]
	s.shapes = s.shapes[:0]
	s.sprites = s.sprites[:0]
	clear(s.texts)
	s.texts = s.texts[:0]
	clear(s.triangles)
	s.triangles = s.triangles[:0]
	s.meshes = s.meshes[:0]
	s.arguments = s.arguments[:0]
	clear(s.spans)
	s.spans = s.spans[:0]
	s.vertexBytes = s.vertexBytes[:0]
	s.indexBytes = s.indexBytes[:0]
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
	if s.frameInspected {
		s.sortTime = s.sortStopwatch.Elapsed()
	}
}

// findSortKeys writes over the sorter's entries each entry with its sort
// key, in the order merged, for the sorter to sort.
func (s *drawState) findSortKeys() {
	s.sorter.entries = s.sorter.entries[:0]
	for index, entry := range s.entries {
		var key sortKey
		switch entry.kind {
		case EntryKindShape:
			shape := &s.shapes[entry.index]
			key = makeSortKey(shape.Layer, shape.Order)
		case EntryKindSprite:
			sprite := &s.sprites[entry.index]
			key = makeSortKey(sprite.Layer, sprite.Order)
		case EntryKindText:
			text := &s.texts[entry.index].text
			key = makeSortKey(text.Layer, text.Order)
		case EntryKindTriangles:
			options := &s.triangles[entry.index].options
			key = makeSortKey(options.Layer, options.Order)
		case EntryKindMesh:
			mesh := &s.meshes[entry.index]
			key = makeSortKey(mesh.Layer, mesh.Order)
		}
		s.sorter.entries = append(s.sorter.entries, sortedEntry{key: key, entryIndex: uint32(index)})
	}
}

// findEntryMaterial answers the 2D material entry, of the frame's, is drawn
// with: a run of text's TextMaterial when it names none.
func (s *drawState) findEntryMaterial(entry recordedEntry) Material {
	switch entry.kind {
	case EntryKindShape:
		return s.shapes[entry.index].Material
	case EntryKindSprite:
		return s.sprites[entry.index].Material
	case EntryKindTriangles:
		return s.triangles[entry.index].options.Material
	case EntryKindMesh:
		return s.meshes[entry.index].Material
	}
	return cmp.Or(s.texts[entry.index].text.Material, TextMaterial)
}

// findEntryImage answers the image entry, of the frame's, draws and the
// frame of it, the zero Image for a shape, which draws the white texel, and
// for a run of text, whose icons each draw their own.
func (s *drawState) findEntryImage(entry recordedEntry) (Image, uint32) {
	switch entry.kind {
	case EntryKindSprite:
		sprite := &s.sprites[entry.index]
		return sprite.Image, sprite.Frame
	case EntryKindTriangles:
		options := &s.triangles[entry.index].options
		return options.Image, options.Frame
	case EntryKindMesh:
		mesh := &s.meshes[entry.index]
		return mesh.Image, mesh.Frame
	}
	return Image{}, 0
}

// findEntryArgument answers the bytes of the argument entry, of the frame's,
// gives its material, none for an entry giving none.
func (s *drawState) findEntryArgument(entry recordedEntry) []byte {
	return s.arguments[entry.argument.offset : entry.argument.offset+findArgumentSize(entry.argument.goType)]
}

// makeInspection answers the frame sorted, of beat, in canvas's terms, as
// drawing drew it through inventory: each camera that drew, and each that
// culled every entry of its layers, in the order the cameras draw, with
// the batches it drew and the entries it culled, each entry with its place
// among the draw lists. It is called
// only for a frame inspected, whose draw lists' ends were kept.
func (s *drawState) makeInspection(beat metronome.Beat, inventory render.Inventory, drawing *renderer) Inspection {
	if !s.frameInspected {
		panic("canvas: making the inspection of a frame not inspected, whose draw lists' ends were not kept")
	}
	inspection := Inspection{Beat: beat}
	culledCameras := drawing.culledCameras
	for index := range drawing.cameraDraws {
		for len(culledCameras) > 0 && culledCameras[0].drawingBefore == index {
			inspection.Cameras = append(inspection.Cameras, s.makeInspectedCamera(inventory, drawing, &culledCameras[0].draw))
			culledCameras = culledCameras[1:]
		}
		inspection.Cameras = append(inspection.Cameras, s.makeInspectedCamera(inventory, drawing, &drawing.cameraDraws[index]))
	}
	for index := range culledCameras {
		inspection.Cameras = append(inspection.Cameras, s.makeInspectedCamera(inventory, drawing, &culledCameras[index].draw))
	}
	return inspection
}

// makeInspectedCamera answers drawnCamera, of the frame drawing drew
// through inventory, as inspected: its batches, each with its material's
// label and its texture's, and the entries of its layers it culled, those
// not among the entries it draws.
func (s *drawState) makeInspectedCamera(inventory render.Inventory, drawing *renderer, drawnCamera *cameraDraw) InspectedCamera {
	targetDescriptor, _ := inventory.TextureDescriptor(drawnCamera.target.texture)
	inspected := InspectedCamera{
		Camera:       drawnCamera.camera,
		Window:       drawnCamera.target.window,
		TextureLabel: targetDescriptor.Label,
		TargetSize:   m.Vec2i{X: drawnCamera.target.width, Y: drawnCamera.target.height},
		Viewport:     drawnCamera.view.viewport,
		Scale:        drawnCamera.view.scale,
		VisibleRect:  drawnCamera.view.findVisibleRect(),
	}
	for _, drawnBatch := range drawnCamera.batches {
		inspectedEntries := make([]InspectedEntry, 0, len(drawnBatch.entries))
		for _, entry := range drawnBatch.entries {
			inspectedEntries = append(inspectedEntries, s.makeInspectedEntry(entry.entryIndex))
		}
		key := drawnBatch.key
		slot, _ := drawing.findMaterial(key.material)
		descriptor, _ := inventory.TextureDescriptor(key.texture.Texture)
		inspected.Batches = append(inspected.Batches, InspectedBatch{
			Material:      key.material,
			MaterialLabel: slot.label,
			Texture:       key.texture.Texture,
			TextureLabel:  descriptor.Label,
			Filter:        key.filter,
			Clip:          key.clip,
			InstanceCount: drawnBatch.instanceCount,
			Entries:       inspectedEntries,
		})
	}
	// The entries the camera draws are those of its layer runs it does not
	// cull, in the same order.
	drawnEntries := drawnCamera.entries
	for _, layerRun := range drawnCamera.camera.findLayerRuns(s.sorter.entries) {
		for _, entry := range layerRun {
			if len(drawnEntries) > 0 && drawnEntries[0].entryIndex == entry.entryIndex {
				drawnEntries = drawnEntries[1:]
				continue
			}
			inspected.Culled = append(inspected.Culled, s.makeInspectedEntry(entry.entryIndex))
		}
	}
	return inspected
}

// makeInspectedEntry answers the entry at entryIndex of the frame's as
// inspected: its kind, the entry as recorded, its sort key, and its place
// among the draw lists.
func (s *drawState) makeInspectedEntry(entryIndex uint32) InspectedEntry {
	// The draw list holding the entry is the first to end after it.
	drawListIndex, _ := slices.BinarySearch(s.drawListEnds, int(entryIndex)+1)
	drawListStart := 0
	if drawListIndex > 0 {
		drawListStart = s.drawListEnds[drawListIndex-1]
	}
	recorded := s.entries[entryIndex]
	inspected := InspectedEntry{Kind: recorded.kind, DrawListIndex: drawListIndex, IndexInDrawList: int(entryIndex) - drawListStart}
	switch recorded.kind {
	case EntryKindShape:
		inspected.Shape = s.shapes[recorded.index]
		inspected.Layer = inspected.Shape.Layer
		inspected.Order = inspected.Shape.Order
	case EntryKindSprite:
		inspected.Sprite = s.sprites[recorded.index]
		inspected.Layer = inspected.Sprite.Layer
		inspected.Order = inspected.Sprite.Order
	case EntryKindText:
		inspected.Text = s.texts[recorded.index].text
		// The next frame's spans are written over these.
		inspected.Text.Spans = slices.Clone(inspected.Text.Spans)
		inspected.Layer = inspected.Text.Layer
		inspected.Order = inspected.Text.Order
	case EntryKindTriangles:
		inspected.TrianglesOptions = s.triangles[recorded.index].options
		inspected.Layer = inspected.TrianglesOptions.Layer
		inspected.Order = inspected.TrianglesOptions.Order
	case EntryKindMesh:
		inspected.Mesh = s.meshes[recorded.index]
		inspected.Layer = inspected.Mesh.Layer
		inspected.Order = inspected.Mesh.Order
	}
	return inspected
}
