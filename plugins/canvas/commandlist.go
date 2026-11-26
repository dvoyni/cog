package canvas

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/render"
)

// CommandList is the task parameter a recording task records its entries
// through, declared in its signature as kernel.Read is:
//
//	func drawUnits(k *kernel.Kernel, event canvas.FrameEvent, list canvas.CommandList) kernel.NoResult
//
// Each task declaring it gets a command list of its own at registration, so
// tasks recording together never contend; the lists are merged when the
// frame is drawn, in registration order: plugin order, then the order each
// plugin registers its tasks. Two jobs of one task in a frame record into
// its list both, in the order their calls run. It locks canvas's draw state
// for read, so recording tasks run together, and the FontLibrary resource
// for read, which lays its text out. It is valid only while the task's job
// runs.
//
// An entry is recorded, and a camera set, only from FrameEvent's Ready until
// canvas's drawing begins; one recorded or set at any other time is
// refused, and reported once for the task, as
// ReportKindRecordedOutsideFrame.
//
// An entry gives its material an argument with the …With calls, of the type
// the material declares with ArgumentOf, checked as canvas draws the frame:
// an argument of another type, or one given a material that takes none, is
// refused, reported once for the material and the type as
// ReportKindArgumentRefused, and the entry skipped. An entry recorded
// without one gives a material that takes one the zero value of its type.
type CommandList struct {
	state kernel.Read[*drawState]
	fonts kernel.Read[FontLibrary]
	list  *commandList
}

// Declare declares CommandList's locks, gives the task being registered a
// command list of its own, after every list given before, and answers
// CommandList bound to it.
func (CommandList) Declare(access *kernel.Access) CommandList {
	state := access.Read[*drawState]()
	return CommandList{state: state, fonts: access.Read[FontLibrary](), list: state.Get().addCommandList()}
}

// DrawShape records shape into the task's command list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l CommandList) DrawShape(shape Shape) {
	l.list.record(l.state.Get(), EntryKindShape, &l.list.shapes, nil, nil, shape)
}

// DrawShapeWith records shape into the task's command list for the frame
// being recorded, as DrawShape does, giving its material argument, of the
// type the material declares with ArgumentOf.
func (l CommandList) DrawShapeWith[T any](shape Shape, argument T) {
	l.list.record(l.state.Get(), EntryKindShape, &l.list.shapes, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)), shape)
}

// DrawSprite records sprite into the task's command list for the frame being
// recorded. Outside the frame, it is refused and reported once for the task.
func (l CommandList) DrawSprite(sprite Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, nil, nil, sprite)
}

// DrawSpriteWith records sprite into the task's command list for the frame
// being recorded, as DrawSprite does, giving its material argument, of the
// type the material declares with ArgumentOf.
func (l CommandList) DrawSpriteWith[T any](sprite Sprite, argument T) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)), sprite)
}

// DrawSprites records sprites into the task's command list for the frame
// being recorded, in their order, as DrawSprite would one after another, so
// an ECS chunk's column goes in as it is. Outside the frame, they are refused
// and reported once for the task.
func (l CommandList) DrawSprites(sprites []Sprite) {
	l.list.record(l.state.Get(), EntryKindSprite, &l.list.sprites, nil, nil, sprites...)
}

// DrawSpritesWith records sprites into the task's command list for the frame
// being recorded, as DrawSprites does, each giving its material the
// argument at its index in arguments, a parallel slice, so an ECS chunk's
// sprite and argument columns go in as they are. Slices of different
// lengths are refused, none of them recorded, and reported once for the
// task as ReportKindSliceLengthsDiffer.
func (l CommandList) DrawSpritesWith[T any](sprites []Sprite, arguments []T) {
	l.list.recordWithArguments(l.state.Get(), EntryKindSprite, &l.list.sprites, sprites, arguments)
}

// DrawText records text, a run of text, into the task's command list for the
// frame being recorded: laid out now through the layout cache, as
// FontLibrary.Layout lays it out, so a label drawn frame after frame is
// shaped once, then drawn as one entry, its glyphs in order, through
// TextMaterial when its Material is zero, which draws its fill alone. Each
// glyph is made a distance field the first time it is drawn, in the calling
// task, unless FontLibrary.Preload made it, and packed into the glyph atlas
// as canvas draws the frame. Outside the frame, it is refused and reported
// once for the task.
func (l CommandList) DrawText(text Text) {
	l.drawText(text, nil, nil)
}

// DrawTextWith records text into the task's command list for the frame being
// recorded, as DrawText does, giving its material argument, of the type the
// material declares with ArgumentOf: TextMaterial's a Profile, drawn under
// the fill, such as canvas.Outline(2, black), its distances in the font's
// field texels.
func (l CommandList) DrawTextWith[T any](text Text, argument T) {
	l.drawText(text, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)))
}

// DrawTriangles records triangles, a mesh given for this frame alone, into
// the task's command list for the frame being recorded, drawn as options say:
// copied now, its vertices and indices into the command list, so the caller
// may reuse its slices, then into the frame's temporary buffers as canvas
// draws the frame, one draw of their own, never merged with another
// entry's. Vertices of canvas.Vertex are drawn by canvas's own mesh vertex
// stage and the material's fragment stage; those of another layout by the
// material's own vertex stage, and are skipped as they are drawn with a
// material whose vertex stage is canvas's, as a fragment-only material's
// is, reported once for the material as ReportKindVertexStageMissing.
// Triangles of no vertices record nothing, and
// those of zero bounds are refused, reported once for the task as
// ReportKindBoundsMissing. Outside the frame, they are refused and reported
// once for the task.
func (l CommandList) DrawTriangles[V meshfile.VertexType](options DrawTrianglesOptions, triangles Triangles[V]) {
	l.drawTriangles(options, triangles, nil, nil)
}

// DrawTrianglesWith records triangles into the task's command list for the
// frame being recorded, as DrawTriangles does, giving its material
// argument, of the type the material declares with ArgumentOf.
func (l CommandList) DrawTrianglesWith[V meshfile.VertexType, T any](options DrawTrianglesOptions, triangles Triangles[V], argument T) {
	l.drawTriangles(options, triangles, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)))
}

// DrawMesh records mesh, a mesh file's triangles, into the task's command
// list for the frame being recorded. Its file loads at its first draw,
// through platform's FS, and draws that frame. Consecutive meshes in sort
// order of one file, material, atlas, filter and clip are one instanced draw.
// A file of another layout than canvas.Vertex's drawn with a material whose
// vertex stage is canvas's is skipped, reported once for the material, as
// Mesh.File says. Outside the frame, it is refused and reported once for the
// task.
func (l CommandList) DrawMesh(mesh Mesh) {
	l.list.record(l.state.Get(), EntryKindMesh, &l.list.meshes, nil, nil, mesh)
}

// DrawMeshWith records mesh into the task's command list for the frame being
// recorded, as DrawMesh does, giving its material argument, of the type the
// material declares with ArgumentOf.
func (l CommandList) DrawMeshWith[T any](mesh Mesh, argument T) {
	l.list.record(l.state.Get(), EntryKindMesh, &l.list.meshes, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&argument, 1)), mesh)
}

// DrawMeshes records meshes into the task's command list for the frame being
// recorded, in their order, as DrawMesh would one after another, so an ECS
// chunk's column goes in as it is, and a forest of one tree, recorded in one
// call, is one draw. Outside the frame, they are refused and reported once
// for the task.
func (l CommandList) DrawMeshes(meshes []Mesh) {
	l.list.record(l.state.Get(), EntryKindMesh, &l.list.meshes, nil, nil, meshes...)
}

// DrawMeshesWith records meshes into the task's command list for the frame
// being recorded, as DrawMeshes does, each giving its material the argument
// at its index in arguments, a parallel slice. Slices of different lengths
// are refused, none of them recorded, and reported once for the task as
// ReportKindSliceLengthsDiffer.
func (l CommandList) DrawMeshesWith[T any](meshes []Mesh, arguments []T) {
	l.list.recordWithArguments(l.state.Get(), EntryKindMesh, &l.list.meshes, meshes, arguments)
}

// SetCamera sets camera for the frame being recorded, forgotten once the
// frame is drawn, as its entries are: nothing is made or kept. A frame that
// sets no camera is seen through the default one, which draws only a frame
// with entries: every layer, into the whole main window, cleared to opaque
// black, at order 0. Outside the frame, it is refused and reported once for
// the task. A camera of a sample count other than 0, 1 and 4 panics.
func (l CommandList) SetCamera(camera Camera) {
	if camera.SampleCount > 1 && camera.SampleCount != multisampledCameraSampleCount {
		panic(fmt.Sprintf("canvas: a camera's SampleCount is 0, 1 or %d, not %d", multisampledCameraSampleCount, camera.SampleCount))
	}
	l.list.record(l.state.Get(), 0, &l.list.cameras, nil, nil, camera)
}

// drawText lays text out and records it, as DrawText does, giving the
// material argument of argumentType in argumentBytes, nil for none: its
// markup read into spans, which recording copies before the shaper whose
// scratch holds them goes back to the pool.
func (l CommandList) drawText(text Text, argumentType reflect.Type, argumentBytes []byte) {
	library := l.fonts.Get().(*fontLibrary)
	shaper := library.acquireShaper(text)
	defer library.releaseShaper(shaper)
	text, layout := library.layOutText(text, true, shaper)
	l.list.recordText(l.state.Get(), argumentType, argumentBytes, laidOutText{text: text, layout: layout})
}

// drawTriangles records triangles drawn with options, as DrawTriangles
// does, giving the material argument of argumentType in argumentBytes, nil
// for none.
func (l CommandList) drawTriangles[V meshfile.VertexType](options DrawTrianglesOptions, triangles Triangles[V], argumentType reflect.Type, argumentBytes []byte) {
	var vertex V
	_, isVertex := any(vertex).(Vertex)
	recorded := recordedTriangles{
		options:        options,
		layout:         vertex.VertexLayout(),
		standardLayout: isVertex,
		topology:       render.PrimitiveTopology(triangles.Topology),
		bounds:         triangles.Bounds,
	}
	l.list.recordTriangles(l.state.Get(), argumentType, argumentBytes, recorded, castToBytes(triangles.Vertices), len(triangles.Vertices), triangles.Indices)
}

// commandList is a frame's commands as recorded: one task's for the frame
// being recorded, which its CommandList records into, or the frame's, every
// task's appended to it in registration order. It holds the entries, in the
// order recorded, each of them a shape, a sprite, a run of text, per-frame
// triangles or a mesh of its records of that kind, with the bytes of the
// material arguments they give, end to end, the spans of its runs of text,
// copied end to end, the vertex and index bytes of its triangles, each end
// to end, and the cameras set, in the order set. Two jobs of one task
// running together share the task's, their calls interleaving.
type commandList struct {
	// outsideFrame is a task's command list's report of an entry recorded or
	// a camera set outside the frame, slicesDiffer of a bulk call given
	// parallel slices of different lengths, and boundsMissing of triangles
	// given zero bounds, each made once; its message is the key it is
	// logged once under. The frame's has none.
	outsideFrame  error
	slicesDiffer  error
	boundsMissing error
	// mutex guards a task's entries, shapes, sprites, texts, triangles,
	// meshes, arguments, spans, vertexBytes, indexBytes and cameras.
	mutex       sync.Mutex
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
	cameras     []Camera
}

// record appends recorded to records, one of the command list's slices, under
// its lock, and an entry of kind for each to its entries, kind zero for
// cameras, which are no entries, each giving its material argument, of
// argumentType, nil for none, its bytes in turn of argumentBytes, unless
// state has no frame open for entries, which it reports once for the command
// list, outside the lock.
func (l *commandList) record[T any](state *drawState, kind EntryKind, records *[]T, argumentType reflect.Type, argumentBytes []byte, recorded ...T) {
	l.mutex.Lock()
	recording := l.appendRecords(state, kind, records, argumentType, argumentBytes, recorded)
	l.mutex.Unlock()
	if !recording {
		state.reporter.reportOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// recordWithArguments records recorded, of kind, as record does, each
// giving its material the argument at its index in arguments, a parallel
// slice, unless the slices' lengths differ, which records none of them and
// is reported once for the command list.
func (l *commandList) recordWithArguments[T, A any](state *drawState, kind EntryKind, records *[]T, recorded []T, arguments []A) {
	if len(recorded) != len(arguments) {
		state.reporter.reportOnce(l.slicesDiffer.Error(), l.slicesDiffer)
		return
	}
	l.record(state, kind, records, reflect.TypeFor[A](), castToBytes(arguments), recorded...)
}

// recordText records text, a run of text laid out, as record does, giving
// the material argument of argumentType in argumentBytes, its spans copied
// into the command list's, so its caller may reuse them once it returns.
func (l *commandList) recordText(state *drawState, argumentType reflect.Type, argumentBytes []byte, text laidOutText) {
	l.mutex.Lock()
	text.text.Spans = appendToArena(&l.spans, text.text.Spans)
	recording := l.appendRecords(state, EntryKindText, &l.texts, argumentType, argumentBytes, []laidOutText{text})
	l.mutex.Unlock()
	if !recording {
		state.reporter.reportOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// recordTriangles records triangles, per-frame triangles of vertexCount
// vertices in vertexBytes and of indices, nil for none, as record does,
// giving the material argument of argumentType in argumentBytes, its
// vertices and indices copied into the command list's, the indices as 16 bits
// while the vertices number at most 65,535, so its caller may reuse them
// once it returns. Triangles of no vertices record nothing, and those of
// zero bounds are refused, reported once for the command list.
func (l *commandList) recordTriangles(state *drawState, argumentType reflect.Type, argumentBytes []byte, triangles recordedTriangles, vertexBytes []byte, vertexCount int, indices []uint32) {
	if vertexCount == 0 {
		return
	}
	if triangles.bounds == (m.Box3{}) {
		state.reporter.reportOnce(l.boundsMissing.Error(), l.boundsMissing)
		return
	}
	l.mutex.Lock()
	triangles.vertices = appendToArena(&l.vertexBytes, vertexBytes)
	indicesStart := len(l.indexBytes)
	switch {
	case len(indices) == 0:
	case vertexCount < 1<<16:
		triangles.indexFormat = render.IndexFormatUint16
		for _, index := range indices {
			l.indexBytes = binary.LittleEndian.AppendUint16(l.indexBytes, uint16(index))
		}
	default:
		triangles.indexFormat = render.IndexFormatUint32
		l.indexBytes = append(l.indexBytes, castToBytes(indices)...)
	}
	triangles.indices = slices.Clip(l.indexBytes[indicesStart:])
	recording := l.appendRecords(state, EntryKindTriangles, &l.triangles, argumentType, argumentBytes, []recordedTriangles{triangles})
	l.mutex.Unlock()
	if !recording {
		state.reporter.reportOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// appendRecords is record's work under the command list's lock: it appends
// recorded to records, and their entries and arguments to the command list's,
// and answers whether state has a frame open for entries, appending
// nothing when it has not.
func (l *commandList) appendRecords[T any](state *drawState, kind EntryKind, records *[]T, argumentType reflect.Type, argumentBytes []byte, recorded []T) bool {
	if !state.recording.Load() {
		return false
	}
	if kind != 0 {
		argumentSize := findArgumentSize(argumentType)
		for index := range recorded {
			argument := recordedArgument{goType: argumentType, offset: uint32(len(l.arguments)) + uint32(index)*argumentSize}
			l.entries = append(l.entries, recordedEntry{kind: kind, index: uint32(len(*records) + index), argument: argument})
		}
		l.arguments = append(l.arguments, argumentBytes...)
	}
	*records = append(*records, recorded...)
	return true
}

// appendToArena appends values, such as a run's spans or a mesh's vertex
// bytes, to arena and answers them as appended there, of no room past their
// end, so nothing appended to arena after reaches them; none answers nil.
func appendToArena[T any](arena *[]T, values []T) []T {
	if len(values) == 0 {
		return nil
	}
	start := len(*arena)
	*arena = append(*arena, values...)
	return slices.Clip((*arena)[start:])
}

// appendTo appends the task's command list's records to mergedList's,
// under its lock, and empties it: each entry's index moved past
// mergedList's records of its kind, its argument's offset past
// mergedList's arguments, and each run's spans and each triangles' vertex
// and index bytes copied into mergedList's arenas.
func (l *commandList) appendTo(mergedList *commandList) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	// recordsBefore are mergedList's records of each kind, at the kind.
	recordsBefore := [...]uint32{
		EntryKindShape:     uint32(len(mergedList.shapes)),
		EntryKindSprite:    uint32(len(mergedList.sprites)),
		EntryKindText:      uint32(len(mergedList.texts)),
		EntryKindTriangles: uint32(len(mergedList.triangles)),
		EntryKindMesh:      uint32(len(mergedList.meshes)),
	}
	argumentsBefore := uint32(len(mergedList.arguments))
	for _, entry := range l.entries {
		entry.index += recordsBefore[entry.kind]
		entry.argument.offset += argumentsBefore
		mergedList.entries = append(mergedList.entries, entry)
	}
	mergedList.shapes = append(mergedList.shapes, l.shapes...)
	mergedList.sprites = append(mergedList.sprites, l.sprites...)
	for _, text := range l.texts {
		text.text.Spans = appendToArena(&mergedList.spans, text.text.Spans)
		mergedList.texts = append(mergedList.texts, text)
	}
	for _, triangles := range l.triangles {
		triangles.vertices = appendToArena(&mergedList.vertexBytes, triangles.vertices)
		triangles.indices = appendToArena(&mergedList.indexBytes, triangles.indices)
		mergedList.triangles = append(mergedList.triangles, triangles)
	}
	mergedList.meshes = append(mergedList.meshes, l.meshes...)
	mergedList.arguments = append(mergedList.arguments, l.arguments...)
	mergedList.cameras = append(mergedList.cameras, l.cameras...)
	l.reset()
}

// reset empties the command list of its records, keeping their room, and
// lets go of what its texts and triangles reach.
func (l *commandList) reset() {
	l.entries = l.entries[:0]
	l.shapes = l.shapes[:0]
	l.sprites = l.sprites[:0]
	clear(l.texts)
	l.texts = l.texts[:0]
	clear(l.triangles)
	l.triangles = l.triangles[:0]
	l.meshes = l.meshes[:0]
	l.arguments = l.arguments[:0]
	clear(l.spans)
	l.spans = l.spans[:0]
	l.vertexBytes = l.vertexBytes[:0]
	l.indexBytes = l.indexBytes[:0]
	l.cameras = l.cameras[:0]
}

// sortKey answers the sort key of entry, of the command list's: its
// record's layer, then its order.
func (l *commandList) sortKey(entry uint32) sortKey {
	recorded := l.entries[entry]
	switch recorded.kind {
	case EntryKindShape:
		shape := &l.shapes[recorded.index]
		return makeSortKey(shape.Layer, shape.Order)
	case EntryKindSprite:
		sprite := &l.sprites[recorded.index]
		return makeSortKey(sprite.Layer, sprite.Order)
	case EntryKindText:
		text := &l.texts[recorded.index].text
		return makeSortKey(text.Layer, text.Order)
	case EntryKindTriangles:
		options := &l.triangles[recorded.index].options
		return makeSortKey(options.Layer, options.Order)
	}
	mesh := &l.meshes[recorded.index]
	return makeSortKey(mesh.Layer, mesh.Order)
}

// material answers the 2D material entry, of the command list's, is drawn
// with: a run of text's TextMaterial when it names none.
func (l *commandList) material(entry uint32) Material {
	recorded := l.entries[entry]
	switch recorded.kind {
	case EntryKindShape:
		return l.shapes[recorded.index].Material
	case EntryKindSprite:
		return l.sprites[recorded.index].Material
	case EntryKindText:
		return cmp.Or(l.texts[recorded.index].text.Material, TextMaterial)
	case EntryKindTriangles:
		return l.triangles[recorded.index].options.Material
	}
	return l.meshes[recorded.index].Material
}

// argument answers the bytes of the argument entry, of the command list's,
// gives its material, none for an entry giving none.
func (l *commandList) argument(entry uint32) []byte {
	argument := l.entries[entry].argument
	return l.arguments[argument.offset : argument.offset+findArgumentSize(argument.goType)]
}

// meshFile answers the mesh file entry, of the command list's, draws, the
// zero MeshFile for an entry of no file.
func (l *commandList) meshFile(entry uint32) MeshFile {
	recorded := l.entries[entry]
	if recorded.kind != EntryKindMesh {
		return MeshFile{}
	}
	return l.meshes[recorded.index].File
}

// findPlaced finds through atlases each image frame and glyph entry, of the
// command list's, draws, marking them drawn, so packing the frame's new
// ones evicts none of them, and noting those missed: a sprite's image's
// frame, per-frame triangles' and a mesh's, a mesh whose file, as meshes
// loaded it, draws nothing finding none, and a run of text's glyphs and
// icons, the glyph atlas made through inventory at the first run.
func (l *commandList) findPlaced(entry uint32, inventory render.Inventory, atlases *atlasSet, meshes []loadedMesh) {
	recorded := l.entries[entry]
	switch recorded.kind {
	case EntryKindSprite:
		sprite := &l.sprites[recorded.index]
		atlases.find(sprite.Image, sprite.Frame)
	case EntryKindText:
		l.texts[recorded.index].findPlaced(inventory, atlases)
	case EntryKindTriangles:
		options := &l.triangles[recorded.index].options
		atlases.find(options.Image, options.Frame)
	case EntryKindMesh:
		mesh := &l.meshes[recorded.index]
		if meshes[mesh.File.index].mesh != (render.Mesh{}) {
			atlases.find(mesh.Image, mesh.Frame)
		}
	}
}

// findDrawnEntry writes into drawn entry, of the command list's, as drawn,
// every field written over: its bounds,
// grown by its material's CullMargin, and its batch key, its material's slot
// of materials, its images and glyphs as atlases placed them, a mesh's file
// as meshes loaded it, and an image naming no filter of defaultFilter; or
// why it is skipped, its bounds and batch key zero. Its material comes
// first, one canvas does not hold, one whose argument's layout mismatched
// its shader's, or one refusing the entry's argument skipping it, then its
// record's: an image's frame or a mesh file drawing nothing, per-frame
// triangles or a mesh of another layout than canvas.Vertex's drawn with a
// material whose vertex stage is canvas's, or a run of text drawing no
// glyph. It reports nothing. It writes through drawn, rather than
// answering it: copying the drawn entry out and into the frame's slice
// made a frame of fills about a tenth slower in canvas's benchmarks.
func (l *commandList) findDrawnEntry(drawn *drawnEntry, entry uint32, materials *frameMaterials, atlases *atlasSet, meshes []loadedMesh, defaultFilter Filter) {
	recorded := l.entries[entry]
	slot, held := materials.find(l.material(entry))
	switch {
	case !held:
		*drawn = drawnEntry{skip: skipReasonMaterialNotHeld}
		return
	case slot.argumentLayoutMismatch:
		*drawn = drawnEntry{skip: skipReasonArgumentLayoutMismatch}
		return
	case !slot.takesArgument(recorded.argument):
		*drawn = drawnEntry{skip: skipReasonArgumentRefused}
		return
	}
	switch recorded.kind {
	case EntryKindShape:
		shape := &l.shapes[recorded.index]
		drawn.bounds = shape.findBounds(l.argument(entry))
		drawn.key = shape.batchKey(atlases.findSpriteAtlas())
	case EntryKindSprite:
		sprite := &l.sprites[recorded.index]
		image := atlases.placed(sprite.Image, sprite.Frame)
		if image.drawsNothing {
			*drawn = drawnEntry{skip: skipReasonFrameDrawsNothing}
			return
		}
		drawn.bounds = sprite.findBounds(image)
		drawn.key = sprite.batchKey(image, defaultFilter)
	case EntryKindText:
		run := &l.texts[recorded.index]
		var drawsGlyphs bool
		if drawn.bounds, drawsGlyphs = run.findBounds(atlases); !drawsGlyphs {
			*drawn = drawnEntry{skip: skipReasonNoGlyphDrawn}
			return
		}
		drawn.key = run.batchKey(atlases.findGlyphAtlas())
	case EntryKindTriangles:
		triangles := &l.triangles[recorded.index]
		image := atlases.placed(triangles.options.Image, triangles.options.Frame)
		if skip := findMeshSkip(image, triangles.standardLayout, slot); skip != skipReasonNone {
			*drawn = drawnEntry{skip: skip}
			return
		}
		drawn.bounds = triangles.findBounds()
		drawn.key = triangles.batchKey(image, defaultFilter, recorded.index)
	case EntryKindMesh:
		mesh := &l.meshes[recorded.index]
		loaded := &meshes[mesh.File.index]
		if loaded.mesh == (render.Mesh{}) {
			*drawn = drawnEntry{skip: skipReasonMeshFileDrawsNothing}
			return
		}
		image := atlases.placed(mesh.Image, mesh.Frame)
		if skip := findMeshSkip(image, loaded.standardLayout, slot); skip != skipReasonNone {
			*drawn = drawnEntry{skip: skip}
			return
		}
		drawn.bounds = mesh.findBounds(loaded)
		drawn.key = mesh.batchKey(image, defaultFilter)
	}
	drawn.bounds = drawn.bounds.Inset(m.Vec2{X: -slot.cullMargin, Y: -slot.cullMargin})
	drawn.key.stride = slot.stride
	drawn.skip = skipReasonNone
}

// appendInstances appends to instances the instance data entry, of the
// command list's, draws, each image frame and glyph as atlases placed it:
// into the stream of stride, its material's, each instance followed by the
// argument it gives, or the zero value, or into the fixed instances for a
// stride of zero. It answers how many instances it appended.
func (l *commandList) appendInstances(instances *listInstanceData, entry uint32, stride uint32, atlases *atlasSet) uint32 {
	recorded := l.entries[entry]
	start := len(instances.fixed)
	switch recorded.kind {
	case EntryKindShape:
		instances.fixed = l.shapes[recorded.index].appendInstances(instances.fixed, atlases)
	case EntryKindSprite:
		instances.fixed = l.sprites[recorded.index].appendInstances(instances.fixed, atlases)
	case EntryKindText:
		instances.fixed = l.texts[recorded.index].appendInstances(instances.fixed, atlases)
	case EntryKindTriangles:
		instances.fixed = l.triangles[recorded.index].appendInstances(instances.fixed, atlases)
	case EntryKindMesh:
		instances.fixed = l.meshes[recorded.index].appendInstances(instances.fixed, atlases)
	}
	count := uint32(len(instances.fixed) - start)
	if stride != 0 {
		instances.moveToStream(start, stride, l.argument(entry))
	}
	return count
}

// inspect answers entry, of the command list's, as inspected: its kind, its
// record as recorded, and its sort key's layer and order, its place among
// the command lists left to its caller.
func (l *commandList) inspect(entry uint32) InspectedEntry {
	recorded := l.entries[entry]
	inspected := InspectedEntry{Kind: recorded.kind}
	switch recorded.kind {
	case EntryKindShape:
		inspected.Shape = l.shapes[recorded.index]
		inspected.Layer = inspected.Shape.Layer
		inspected.Order = inspected.Shape.Order
	case EntryKindSprite:
		inspected.Sprite = l.sprites[recorded.index]
		inspected.Layer = inspected.Sprite.Layer
		inspected.Order = inspected.Sprite.Order
	case EntryKindText:
		inspected.Text = l.texts[recorded.index].text
		// The next frame's spans are written over these.
		inspected.Text.Spans = slices.Clone(inspected.Text.Spans)
		inspected.Layer = inspected.Text.Layer
		inspected.Order = inspected.Text.Order
	case EntryKindTriangles:
		inspected.TrianglesOptions = l.triangles[recorded.index].options
		inspected.Layer = inspected.TrianglesOptions.Layer
		inspected.Order = inspected.TrianglesOptions.Order
	case EntryKindMesh:
		inspected.Mesh = l.meshes[recorded.index]
		inspected.Layer = inspected.Mesh.Layer
		inspected.Order = inspected.Mesh.Order
	}
	return inspected
}

// recordedEntry is one entry of a command list, a task's or the frame's:
// its kind, its place among the records of that kind beside it, and the
// material argument it gives.
type recordedEntry struct {
	kind     EntryKind
	index    uint32
	argument recordedArgument
}

// recordedArgument is the material argument an entry gives, as recorded:
// its Go type, nil for none, and where its bytes begin among the arguments
// beside it, its command list's.
type recordedArgument struct {
	goType reflect.Type
	offset uint32
}
