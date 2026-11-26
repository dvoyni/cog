package canvas

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material and the clear and
// composite materials, made at canvas's first draw; the atlas set, which
// keeps what the entries draw from; the mesh files drawn; and what each
// draw reuses from the frame before, so a steady frame allocates nothing:
// the cameras drawn, the entries' keys, the instance data and the batches.
type renderer struct {
	// defaultSlot, sdfSlot and textSlot, the default material's,
	// SDFMaterial's and TextMaterial's, clearMaterial and compositeMaterial
	// are zero until made.
	defaultSlot       materialSlot
	sdfSlot           materialSlot
	textSlot          materialSlot
	clearMaterial     render.Material
	compositeMaterial render.Material
	// atlases keeps the atlases and where each image frame and glyph drawn
	// lies in them.
	atlases atlasSet
	// unloadedPaths are the paths the frame's residency requests unload, in
	// the order asked, written over each frame.
	unloadedPaths []string
	// cameraDraws are the frame's cameras that draw, in the order they
	// draw, and mainWindow the main window they were drawn against, the
	// target of those naming none.
	cameraDraws []cameraDraw
	mainWindow  platform.Window
	// culledCameras are the frame's cameras that draw nothing, culling every
	// entry of their layers, or having none, and not clearing, kept only
	// for a frame inspected, which inspects them too.
	culledCameras []culledCamera
	// materialSlots are the slots of the 2D materials Assets made, by index,
	// as they stood as the frame began drawing, written over each frame.
	materialSlots []materialSlot
	// entryBounds, batchKeys and skippedEntries are the bounds, the batch
	// keys and whether they are skipped, drawing an image's frame that draws
	// nothing or with a material canvas does not hold, of the frame's
	// entries, each at its entry's index, and visibleEntries the entries
	// each camera draws, in turn, all written over each frame.
	entryBounds    []m.Rect
	batchKeys      []batchKey
	skippedEntries []bool
	visibleEntries []sortedEntry
	// instances are the instance data of the command list being drawn, and
	// batches the frame's batches, each camera's in turn; both are written
	// over each frame.
	instances listInstanceData
	batches   []batch
	// loadedMeshes are the mesh files drawn, each at its MeshFile's index,
	// loaded at its first draw and kept until its path is unloaded; the zero
	// MeshFile's, at 0, draws nothing.
	loadedMeshes []loadedMesh
	// listMeshes are the temporary meshes of the frame's per-frame
	// triangles, each at its triangles' index, acquired from the command
	// list being drawn at the first of its cameras drawing them, zero until
	// then, written over for each command list, and vertexAttributes and
	// vertexBuffers the layout of the mesh made or acquired last.
	listMeshes       []render.Mesh
	vertexAttributes []render.VertexAttribute
	vertexBuffers    [1]render.VertexBuffer
	// colorTargets is a camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// newRenderer answers a renderer that has drawn nothing, its atlas set
// made of its parameters, as newAtlasSet makes it.
func newRenderer(atlasPageSize, atlasLayerCeiling, ownTextureSize, mipLevelCount uint32) *renderer {
	return &renderer{
		atlases:      newAtlasSet(atlasPageSize, atlasLayerCeiling, ownTextureSize, mipLevelCount),
		loadedMeshes: []loadedMesh{{tried: true}},
	}
}

// init makes the default material, over the default shader, which it keeps
// acquired, blending premultiplied colour over what the target holds;
// SDFMaterial's and TextMaterial's slots, their arguments' layouts checked,
// a mismatch reported once; the
// clear material, over the viewport shader, which it keeps acquired too,
// replacing what the target holds, which draws a camera's clear over its
// viewport, and what a multisampled camera that clears resolved; the
// composite material, over the viewport shader, blending as the default
// material does, which draws what a multisampled camera that does not clear
// resolved; and the atlas set's pack material, over the viewport shader,
// and its sRGB sprite atlas, the white texel packed into it first.
func (r *renderer) init(inventory render.Inventory, reporter *reporter) {
	r.defaultSlot = makeMaterialSlot(inventory, reporter, MaterialDescriptor{Label: "canvas default", Shader: defaultShaderPath})
	r.sdfSlot = makeMaterialSlot(inventory, reporter, sdfMaterialDescriptor)
	r.textSlot = makeMaterialSlot(inventory, reporter, textMaterialDescriptor)
	viewportShader := inventory.AcquireShader(viewportShaderPath)
	r.clearMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas clear",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: viewportFragmentEntryPoint,
	})
	var compositeState render.State
	compositeState.ColorTargets[0].Blend = BlendNormal()
	r.compositeMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas composite",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: viewportFragmentEntryPoint,
		State:              compositeState,
	})
	r.atlases.init(inventory, viewportShader)
}

// drawCameras draws the entries state sorted through each of its cameras,
// mainWindow the target of those naming no other: one command list for each
// distinct order, its cameras' passes in the order they draw, each a pass
// into its target over its viewport, cleared there when it clears, then an
// instanced draw of each batch of the entries its layers hold over their
// atlas, each batch scissored to its clip, a
// multisampled camera's into a texture of its viewport then resolved and
// drawn over its viewport. A camera culls the entries whose bounds miss its
// view, or their clip's part of it; one drawing no entry that does not
// clear draws nothing, nor does one makeCameraDraw finds draws nothing. It
// makes the materials and the sRGB atlas at the first frame setting a
// camera, and loads a mesh's file from files at the first frame recording
// it. The atlas set finds each entry's images and glyphs, the glyph atlas
// made at the first run of text, keying the entry while nothing has
// missed, then places what missed, a glyph's field read
// from library's glyph table, a converted image's file read from files,
// decoded and converted, keying every entry again if an atlas moved, and
// the rest once placed; the lowest order's command list records the
// uploads before any camera's pass. Once each entry's batch key is found,
// it regroups state's sorted entries by them. First, whether a camera is
// set or not, it begins the atlas set's frame and unloads the paths state's
// residency requests ask, the mesh files of those paths among them.
func (r *renderer) drawCameras(frame render.Frame, inventory render.Inventory, files fs.FS, manager *platform.WindowManager, mainWindow platform.Window, library *fontLibrary, state *drawState) {
	r.cameraDraws = r.cameraDraws[:0]
	clear(r.culledCameras)
	r.culledCameras = r.culledCameras[:0]
	r.mainWindow = mainWindow
	r.batches = r.batches[:0]
	r.visibleEntries = r.visibleEntries[:0]
	r.entryBounds = r.entryBounds[:0]
	r.batchKeys = r.batchKeys[:0]
	r.skippedEntries = r.skippedEntries[:0]
	r.atlases.beginFrame()
	r.unloadedPaths = state.residency.takeUnloadedPaths(r.unloadedPaths[:0])
	for _, unloaded := range r.unloadedPaths {
		r.atlases.unload(inventory, unloaded)
		r.unloadMeshFiles(inventory, unloaded)
	}
	// A frame of entries always sets a camera, the default one standing in.
	if len(state.frameCommandList.cameras) == 0 {
		return
	}
	if r.defaultSlot.material == (render.Material{}) {
		r.init(inventory, state.reporter)
	}
	r.materialSlots = state.materials.appendSlots(r.materialSlots[:0])
	// Every image and glyph the frame draws is marked drawn before any is
	// packed, so packing the frame's new ones evicts none the frame draws.
	for _, entry := range state.frameCommandList.entries {
		r.findEntryFramesAndGlyphs(inventory, files, state, entry)
		if !r.atlases.missed() {
			r.appendEntryKeys(state, entry)
		}
	}
	// Growing an atlas replaces its texture, so every key is found again.
	if r.atlases.placeMissed(inventory, files, &library.glyphs, state.reporter, &state.residency, &state.fileStamps, state.inspector) {
		r.entryBounds = r.entryBounds[:0]
		r.batchKeys = r.batchKeys[:0]
		r.skippedEntries = r.skippedEntries[:0]
	}
	for _, entry := range state.frameCommandList.entries[len(r.batchKeys):] {
		r.appendEntryKeys(state, entry)
	}
	state.sorter.regroup(r.batchKeys)
	if slices.Contains(r.skippedEntries, true) {
		state.sorter.entries = slices.DeleteFunc(state.sorter.entries, func(entry sortedEntry) bool {
			return r.skippedEntries[entry.entryIndex]
		})
	}
	for index := range state.frameCommandList.cameras {
		camera := &state.frameCommandList.cameras[index]
		drawnCamera, ok := makeCameraDraw(inventory, manager, mainWindow, state.reporter, camera)
		if !ok {
			continue
		}
		entriesStart := len(r.visibleEntries)
		for _, layerRun := range camera.findLayerRuns(state.sorter.entries) {
			r.visibleEntries = drawnCamera.view.appendVisibleEntries(r.visibleEntries, layerRun, r.batchKeys, r.entryBounds)
		}
		drawnCamera.entries = r.visibleEntries[entriesStart:]
		switch {
		case camera.Clear || len(drawnCamera.entries) > 0:
			r.cameraDraws = append(r.cameraDraws, drawnCamera)
		case state.frameInspected:
			r.culledCameras = append(r.culledCameras, culledCamera{drawingBefore: len(r.cameraDraws), draw: drawnCamera})
		}
	}
	for orderStart := 0; orderStart < len(r.cameraDraws); {
		orderEnd := orderStart + 1
		for orderEnd < len(r.cameraDraws) && r.cameraDraws[orderEnd].camera.Order == r.cameraDraws[orderStart].camera.Order {
			orderEnd++
		}
		r.drawOrder(frame, inventory, r.cameraDraws[orderStart:orderEnd], state)
		orderStart = orderEnd
	}
}

// findEntryFramesAndGlyphs finds, through the atlas set, the image frames
// and glyphs entry, of state's, draws, marking them drawn: a sprite's
// image's frame, per-frame triangles' and a mesh's, loading the mesh's file
// from files at its first draw, a mesh whose file draws nothing finding
// none; and a run of text's glyphs and icons.
func (r *renderer) findEntryFramesAndGlyphs(inventory render.Inventory, files fs.FS, state *drawState, entry recordedEntry) {
	switch entry.kind {
	case EntryKindMesh:
		file := state.frameCommandList.meshes[entry.index].File
		if r.loadMesh(inventory, files, state.reporter, &state.fileStamps, file) {
			state.inspector.addLoadedMeshFile(file)
		}
		if r.loadedMeshes[file.index].mesh == (render.Mesh{}) {
			return
		}
		fallthrough
	case EntryKindSprite, EntryKindTriangles:
		r.atlases.find(state.findEntryImage(entry))
	case EntryKindText:
		layout := &state.frameCommandList.texts[entry.index].layout
		r.atlases.findGlyphs(inventory, layout.glyphs)
		for _, icon := range layout.spanIcons {
			if icon != nil {
				r.atlases.find(icon.image, icon.frame)
			}
		}
	}
}

// drawOrder draws cameraDraws, the cameras of one order in the order they
// draw, of state's entries, as one command list at that order: first the
// atlas set's uploads, which the lowest order's list records, leaving none
// for the others, then each camera's clear drawn over its viewport,
// or a multisampled
// camera's quad drawing what it resolved there, and its batches as
// instance data, in one temporary buffer, then each camera's entries'
// pass, a multisampled camera's textures acquired before it and its
// composite after it. The instance data is in temporary buffers, one per
// stride. A multisampled camera whose textures render could not make, which
// it logged, draws nothing.
func (r *renderer) drawOrder(frame render.Frame, inventory render.Inventory, cameraDraws []cameraDraw, state *drawState) {
	r.instances.reset()
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		switch {
		case drawnCamera.camera.SampleCount == multisampledCameraSampleCount:
			// What it resolved into is drawn whole, untinted.
			resolved := drawnImage{region: m.Rect{Width: 1, Height: 1}}
			drawnCamera.viewportInstance = uint32(len(r.instances.fixed))
			r.instances.fixed = appendShapeInstances(r.instances.fixed, &Shape{Tint: m.White}, &resolved)
		case drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear:
			// A clear not by the pass's load is drawn over the viewport, whose
			// vertex stage reads only the instance's tint, region and page.
			drawnCamera.viewportInstance = uint32(len(r.instances.fixed))
			r.instances.fixed = appendShapeInstances(r.instances.fixed, &Shape{Tint: drawnCamera.camera.ClearColor}, r.atlases.placed(Image{}, 0))
		}
		batchesStart := len(r.batches)
		r.batches = appendBatches(r.batches, &r.instances, drawnCamera.entries, state, r.batchKeys, &r.atlases)
		drawnCamera.batches = r.batches[batchesStart:]
	}
	list := frame.BeginCommandList("canvas", cameraDraws[0].camera.Order)
	r.instances.acquireBuffers(&list)
	r.acquireListMeshes(&list, cameraDraws, state)
	r.atlases.recordUploads(&list, inventory)
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		if drawnCamera.camera.SampleCount != multisampledCameraSampleCount {
			r.drawEntries(&list, drawnCamera, state)
			continue
		}
		descriptor := drawnCamera.multisampledDescriptor
		drawnCamera.colorTarget.Texture = frame.AcquireTemporaryTexture(descriptor)
		descriptor.Label = "canvas resolved camera"
		descriptor.SampleCount = 1
		drawnCamera.colorTarget.Resolve.Texture = frame.AcquireTemporaryTexture(descriptor)
		if drawnCamera.colorTarget.Texture == (render.Texture{}) || drawnCamera.colorTarget.Resolve.Texture == (render.Texture{}) {
			continue
		}
		r.drawEntries(&list, drawnCamera, state)
		r.drawComposite(&list, drawnCamera)
	}
	list.End()
}

// drawEntries draws drawnCamera's entries, of state's, in a pass into its
// colour target, the viewport and the scissors moved from the target's
// pixels to the colour target's by its colorTargetOrigin. Its clear, when
// not by the pass's load, is drawn over the viewport first, then each
// batch, from the instance data of its material's stride, scissored to the
// part of the viewport the entries draw into, and a clipped batch to its
// clip's part of that, a batch whose clip holds no pixel of it not drawn.
// A batch of per-frame triangles draws their temporary mesh, and a batch of
// meshes of one file the mesh loaded of it, an instance for each, both
// through the material's mesh vertex stage for canvas.Vertex's layout, and
// its own vertex stage for any other; one whose mesh render refused draws
// nothing.
func (r *renderer) drawEntries(list *render.CommandList, drawnCamera *cameraDraw, state *drawState) {
	r.colorTargets[0] = drawnCamera.colorTarget
	pass := list.BeginPass(render.PassDescriptor{Label: "canvas camera", ColorTargets: r.colorTargets[:]})
	toColorTarget := drawnCamera.colorTargetOrigin.Negate()
	viewport := drawnCamera.view.viewport.Translate(toColorTarget)
	pass.SetViewport(render.Viewport{X: float32(viewport.X), Y: float32(viewport.Y), Width: float32(viewport.Width), Height: float32(viewport.Height), MaxDepth: 1})
	pass.SetPassUniform("canvas_camera", drawnCamera.block)
	// A run of text draws its icons from the sRGB sprite atlas, beside the
	// glyph atlas.
	spriteAtlas := r.atlases.findSpriteAtlas()
	if drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear {
		drawInstances(&pass, r.clearMaterial, r.instances.fixedRange, drawnCamera.viewportInstance, 1, spriteAtlas, render.TextureView{}, FilterNearest, WrapNone)
	}
	for _, drawnBatch := range drawnCamera.batches {
		// The entries draw between FitInscribe's bars, which the clear
		// covered, and within their clip.
		scissor, clipDrawn := drawnCamera.view.placeClip(drawnBatch.key.clip)
		if !clipDrawn {
			continue
		}
		scissor = scissor.Translate(toColorTarget)
		pass.SetScissorRectangle(render.ScissorRectangle{X: uint32(scissor.X), Y: uint32(scissor.Y), Width: uint32(scissor.Width), Height: uint32(scissor.Height)})
		material, _ := r.findMaterial(drawnBatch.key.material)
		instances := r.instances.findRange(drawnBatch.key.stride)
		key := drawnBatch.key
		var mesh render.Mesh
		var standardLayout bool
		switch {
		case key.triangles != 0:
			mesh = r.listMeshes[key.triangles-1]
			standardLayout = state.frameCommandList.triangles[key.triangles-1].standardLayout
		case key.meshFile != (MeshFile{}):
			loaded := &r.loadedMeshes[key.meshFile.index]
			mesh = loaded.mesh
			standardLayout = loaded.standardLayout
		default:
			drawInstances(&pass, material.material, instances, drawnBatch.firstInstance, drawnBatch.instanceCount, key.texture, spriteAtlas, key.filter, key.wrap)
			continue
		}
		if mesh == (render.Mesh{}) {
			continue
		}
		renderMaterial := material.material
		if standardLayout {
			renderMaterial = material.meshMaterial
		}
		bindInstances(&pass, renderMaterial, instances, key.texture, spriteAtlas, key.filter, key.wrap)
		pass.Draw(mesh, render.DrawArgs{InstanceCount: drawnBatch.instanceCount, FirstInstance: drawnBatch.firstInstance})
	}
	pass.End()
}

// acquireListMeshes acquires from list the temporary mesh of each of
// state's per-frame triangles that a batch of cameraDraws draws, once each,
// over temporary buffers of their vertex and index bytes, their layout
// translated into render's, forgetting those of the list before.
func (r *renderer) acquireListMeshes(list *render.CommandList, cameraDraws []cameraDraw, state *drawState) {
	r.listMeshes = slices.Grow(r.listMeshes[:0], len(state.frameCommandList.triangles))[:len(state.frameCommandList.triangles)]
	clear(r.listMeshes)
	for index := range cameraDraws {
		for _, drawnBatch := range cameraDraws[index].batches {
			trianglesIndex := drawnBatch.key.triangles
			if trianglesIndex == 0 || r.listMeshes[trianglesIndex-1] != (render.Mesh{}) {
				continue
			}
			triangles := &state.frameCommandList.triangles[trianglesIndex-1]
			r.setVertexBuffer(list.AcquireTemporaryBuffer(triangles.vertices), triangles.layout)
			descriptor := render.MeshDescriptor{Label: "canvas triangles", VertexBuffers: r.vertexBuffers[:], Topology: triangles.topology}
			if len(triangles.indices) > 0 {
				descriptor.Indices = render.IndexBuffer{Range: list.AcquireTemporaryBuffer(triangles.indices), Format: triangles.indexFormat}
			}
			r.listMeshes[trianglesIndex-1] = list.AcquireTemporaryMesh(descriptor)
		}
	}
}

// setVertexBuffer sets the renderer's one vertex buffer to vertices, laid
// out as layout says, translated into render's layout.
func (r *renderer) setVertexBuffer(vertices render.BufferRange, layout meshfile.Layout) {
	r.vertexAttributes = r.vertexAttributes[:0]
	for _, attribute := range layout.Attributes {
		r.vertexAttributes = append(r.vertexAttributes, render.VertexAttribute{Format: render.VertexFormat(attribute.Format), Offset: uint64(attribute.Offset), Location: attribute.Location})
	}
	r.vertexBuffers[0] = render.VertexBuffer{Range: vertices, Layout: render.VertexLayout{Stride: uint64(layout.Stride), Attributes: r.vertexAttributes}}
}

// drawComposite draws what drawnCamera, a multisampled camera, resolved its
// entries into over its viewport, in a pass into its target:
// replacing what the viewport holds when the camera clears, the pass's load
// clearing the target when the viewport covers it, and blended over it
// otherwise. Its colour was encoded as the target's as its entries drew, so
// it is drawn as it is.
func (r *renderer) drawComposite(list *render.CommandList, drawnCamera *cameraDraw) {
	r.colorTargets[0] = drawnCamera.makeTargetColorTarget()
	pass := list.BeginPass(render.PassDescriptor{Label: "canvas camera composite", ColorTargets: r.colorTargets[:]})
	viewport := drawnCamera.view.viewport
	pass.SetViewport(render.Viewport{X: float32(viewport.X), Y: float32(viewport.Y), Width: float32(viewport.Width), Height: float32(viewport.Height), MaxDepth: 1})
	// The viewport vertex stage reads no projection, and the zero camera
	// encodes nothing.
	pass.SetPassUniform("canvas_camera", cameraBlock{})
	material := r.compositeMaterial
	if drawnCamera.camera.Clear {
		material = r.clearMaterial
	}
	drawInstances(&pass, material, r.instances.fixedRange, drawnCamera.viewportInstance, 1, render.TextureView{Texture: drawnCamera.colorTarget.Resolve.Texture}, render.TextureView{}, FilterNearest, WrapNone)
	pass.End()
}

// drawInstances draws instanceCount instances of instances from
// firstInstance into pass with material, each a quad's six vertices, as
// bindInstances binds them.
func drawInstances(pass *render.Pass, material render.Material, instances render.BufferRange, firstInstance, instanceCount uint32, texture, spriteAtlas render.TextureView, filter Filter, wrap Wrap) {
	bindInstances(pass, material, instances, texture, spriteAtlas, filter, wrap)
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: instanceCount, FirstInstance: firstInstance})
}

// bindInstances selects material in pass, and binds instances, texture, and
// spriteAtlas beside it unless it is zero, read through filter, repeating
// along the axes of wrap, for the draw after it. Setting a material resets
// the draw parameters, so each draw sets its own.
func bindInstances(pass *render.Pass, material render.Material, instances render.BufferRange, texture, spriteAtlas render.TextureView, filter Filter, wrap Wrap) {
	pass.SetMaterial(material)
	pass.SetBuffer(instancesBindingName, instances)
	pass.SetTextureView("canvas_atlas", texture)
	if spriteAtlas != (render.TextureView{}) {
		pass.SetTextureView("canvas_sprite_atlas", spriteAtlas)
	}
	pass.SetSampler("canvas_sampler", filter.makeSamplerDescriptor(wrap))
}

// appendEntryKeys appends entry's bounds, grown by its material's
// CullMargin, its batch key, and that it is not skipped, of state's, to the
// frame's, its image placed: packed, given a view of its texture, or found
// to draw nothing. An entry skipped, its image's frame drawing nothing, its
// material one canvas does not hold, or its argument one its material does
// not take, reported once, or of a material whose
// argument's layout mismatched its shader's, or a mesh whose file draws
// nothing, or per-frame triangles or a mesh of another layout than
// canvas.Vertex's whose material has no vertex stage of its own, reported
// once, has zero bounds and batch key.
func (r *renderer) appendEntryKeys(state *drawState, entry recordedEntry) {
	material := state.findEntryMaterial(entry)
	slot, held := r.findMaterial(material)
	if !held {
		reportMaterialNotHeld(state.reporter, material, "drawing an entry with")
		r.appendSkippedEntryKeys()
		return
	}
	// A material whose argument's layout mismatched its shader's, reported
	// as it was made, draws nothing.
	if slot.argumentLayoutMismatch || !slot.takesArgument(state.reporter, material, entry.argument) {
		r.appendSkippedEntryKeys()
		return
	}
	margin := m.Vec2{X: -slot.cullMargin, Y: -slot.cullMargin}
	var bounds m.Rect
	var key batchKey
	switch entry.kind {
	case EntryKindShape:
		shape := &state.frameCommandList.shapes[entry.index]
		if material == SDFMaterial {
			bounds = shape.findProfileBounds(profileFromBytes(state.findEntryArgument(entry)))
		} else {
			bounds = shape.findBounds()
		}
		key = shape.batchKey(r.atlases.findSpriteAtlas())
	case EntryKindSprite:
		sprite := &state.frameCommandList.sprites[entry.index]
		image := r.atlases.placed(sprite.Image, sprite.Frame)
		if image.drawsNothing {
			r.appendSkippedEntryKeys()
			return
		}
		bounds = sprite.findBounds(image)
		key = sprite.batchKey(image, state.defaultFilter)
	case EntryKindText:
		run := &state.frameCommandList.texts[entry.index]
		var drawsGlyphs bool
		if bounds, drawsGlyphs = run.findBounds(&r.atlases); !drawsGlyphs {
			r.appendSkippedEntryKeys()
			return
		}
		key = run.batchKey(r.atlases.findGlyphAtlas())
	case EntryKindTriangles:
		triangles := &state.frameCommandList.triangles[entry.index]
		image := r.atlases.placed(triangles.options.Image, triangles.options.Frame)
		if image.drawsNothing || !slot.drawsLayout(state.reporter, material, triangles.standardLayout) {
			r.appendSkippedEntryKeys()
			return
		}
		box := triangles.bounds
		bounds = cmp.Or(triangles.options.Transform, m.Translation2x3(0, 0)).TransformRect(m.Rect{X: box.Min.X, Y: box.Min.Y, Width: box.Max.X - box.Min.X, Height: box.Max.Y - box.Min.Y})
		key = triangles.batchKey(image, state.defaultFilter, entry.index)
	case EntryKindMesh:
		mesh := &state.frameCommandList.meshes[entry.index]
		loaded := &r.loadedMeshes[mesh.File.index]
		if loaded.mesh == (render.Mesh{}) {
			r.appendSkippedEntryKeys()
			return
		}
		image := r.atlases.placed(mesh.Image, mesh.Frame)
		if image.drawsNothing || !slot.drawsLayout(state.reporter, material, loaded.standardLayout) {
			r.appendSkippedEntryKeys()
			return
		}
		box := loaded.bounds
		bounds = cmp.Or(mesh.Transform, m.Translation2x3(0, 0)).TransformRect(m.Rect{X: box.Min.X, Y: box.Min.Y, Width: box.Max.X - box.Min.X, Height: box.Max.Y - box.Min.Y})
		key = makeImageBatchKey(mesh.Material, mesh.Image, image, mesh.Clip, mesh.Filter, state.defaultFilter)
		key.meshFile = mesh.File
	}
	key.stride = slot.stride
	r.entryBounds = append(r.entryBounds, bounds.Inset(margin))
	r.batchKeys = append(r.batchKeys, key)
	r.skippedEntries = append(r.skippedEntries, false)
}

// appendSkippedEntryKeys appends a skipped entry's zero bounds and batch key
// to the frame's, and that it is skipped.
func (r *renderer) appendSkippedEntryKeys() {
	r.entryBounds = append(r.entryBounds, m.Rect{})
	r.batchKeys = append(r.batchKeys, batchKey{})
	r.skippedEntries = append(r.skippedEntries, true)
}

// findMaterial answers the slot of material as the frame began drawing, and
// whether canvas holds it: the default material's for the zero Material,
// SDFMaterial's and TextMaterial's, made at canvas's first draw, and a released
// material's until its slot is freed, once the frame of its release is
// drawn.
func (r *renderer) findMaterial(material Material) (materialSlot, bool) {
	switch material {
	case Material{}:
		return r.defaultSlot, true
	case SDFMaterial:
		return r.sdfSlot, true
	case TextMaterial:
		return r.textSlot, true
	}
	slot, found := findSlot(r.materialSlots, material)
	if !found {
		return materialSlot{}, false
	}
	return *slot, true
}

// loadMesh loads file at its first draw: stamped in stamps, read from files and decoded, its
// vertices and indices made into buffers and a mesh over them through
// inventory, kept at its index among the loaded meshes. A file that is missing, is no mesh file,
// is of a version canvas does not know, is cut short or has zero bounds
// draws nothing, reported once for its path, as does one whose
// mesh render refused, which render reports. It answers whether it loaded
// file now.
func (r *renderer) loadMesh(inventory render.Inventory, files fs.FS, reporter *reporter, stamps *fileStampTable, file MeshFile) bool {
	if int(file.index) >= len(r.loadedMeshes) {
		r.loadedMeshes = slices.Grow(r.loadedMeshes, int(file.index)+1-len(r.loadedMeshes))[:file.index+1]
	}
	loaded := &r.loadedMeshes[file.index]
	if loaded.tried {
		return false
	}
	loaded.tried = true
	path := meshFileNames.findKeyAt(file.index)
	stamps.add(files, path)
	data, err := fs.ReadFile(files, path)
	var decoded meshfile.File
	if err == nil {
		decoded, err = meshfile.Decode(data)
	}
	if err == nil && decoded.Bounds == (m.Box3{}) {
		err = errors.New("its bounds are zero, and a mesh needs them given, or it would vanish as its origin left a view")
	}
	if err != nil {
		notLoaded := Report{Kind: ReportKindMeshFileNotLoaded, Err: fmt.Errorf("canvas: loading mesh file %q, which draws nothing: %w", path, err)}
		reporter.reportOnce(meshFileNotLoadedLogKey+path, notLoaded)
		return false
	}
	loaded.bounds = decoded.Bounds
	loaded.standardLayout = decoded.Layout.Stride == vertexLayout.Stride && slices.Equal(decoded.Layout.Attributes, vertexLayout.Attributes)
	loaded.vertexBuffer = inventory.MakeBuffer(path, render.BufferUsageFlagVertex, decoded.Vertices)
	r.setVertexBuffer(render.BufferRange{Buffer: loaded.vertexBuffer}, decoded.Layout)
	descriptor := render.MeshDescriptor{Label: path, VertexBuffers: r.vertexBuffers[:], Topology: render.PrimitiveTopology(decoded.Topology)}
	if decoded.IndexCount > 0 {
		loaded.indexBuffer = inventory.MakeBuffer(path, render.BufferUsageFlagIndex, decoded.Indices)
		descriptor.Indices = render.IndexBuffer{Range: render.BufferRange{Buffer: loaded.indexBuffer}, Format: render.IndexFormat(decoded.IndexFormat)}
	}
	loaded.mesh = inventory.MakeMesh(descriptor)
	return true
}

// unloadMeshFiles releases each mesh file loaded of the paths
// Unload(prefix) unloads, or forgets that it failed, so its next draw loads
// it again.
func (r *renderer) unloadMeshFiles(inventory render.Inventory, prefix string) {
	for index := 1; index < len(r.loadedMeshes); index++ {
		loaded := &r.loadedMeshes[index]
		if loaded.tried && isUnder(meshFileNames.findKeyAt(uint32(index)), prefix) {
			loaded.unload(inventory)
		}
	}
}

// startAfresh starts the renderer afresh once render has recovered a lost
// device, which left every texture and buffer canvas made empty under its
// handle: the atlas set starts afresh, and each mesh file loaded, or
// failed, is released and forgotten, so its next draw loads it again.
func (r *renderer) startAfresh(inventory render.Inventory) {
	r.atlases.startAfresh(inventory)
	for index := 1; index < len(r.loadedMeshes); index++ {
		r.loadedMeshes[index].unload(inventory)
	}
}

// culledCamera is a camera of a frame inspected that draws nothing, as it
// would draw, after drawingBefore of the cameras that draw.
type culledCamera struct {
	drawingBefore int
	draw          cameraDraw
}
