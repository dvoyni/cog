package canvas

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material, the clear, composite and
// pack materials and the sRGB sprite atlas, holding the white texel shapes
// sample, made at canvas's first draw; the linear sprite atlas, made at the
// first image needing it; each image drawn so far, and those packed whose
// cells are still to fill; and what each draw reuses from the frame
// before, so a steady frame allocates nothing: the cameras drawn, the
// entries' keys, the instance data and the batches.
type renderer struct {
	// atlasPageSize is the side of a sprite atlas's pages,
	// Config.AtlasPageSize's, never zero.
	atlasPageSize uint32
	// defaultMaterial, clearMaterial, compositeMaterial, packMaterial and
	// srgbAtlas are zero until made, and linearAtlas until an image needs
	// it.
	defaultMaterial   render.Material
	clearMaterial     render.Material
	compositeMaterial render.Material
	packMaterial      render.Material
	srgbAtlas         *atlas
	linearAtlas       *atlas
	// drawnImages are the images drawn, each at its Image's index, the
	// zero Image's the white texel's, and one not yet drawn of no size;
	// packedImages are those packed, of no pivot, by their key of the
	// options that change their pixels alone, which images differing only
	// in their pivot share.
	drawnImages  []drawnImage
	packedImages map[imageKey]drawnImage
	// packings are the images packed whose cells a command list is still
	// to fill, kept from the frame that packed them until one does.
	packings []atlasPacking
	// cameraDraws are the frame's cameras that draw, in the order they
	// draw, and mainWindow the main window they were drawn against, the
	// target of those naming none.
	cameraDraws []cameraDraw
	mainWindow  platform.Window
	// entryBounds and batchKeys are the bounds and the batch keys of the
	// frame's entries, each at its entry's index, and visibleEntries the
	// entries each camera draws, in turn, all written over each frame.
	entryBounds    []m.Rect
	batchKeys      []batchKey
	visibleEntries []sortedEntry
	// instances are the instance data of the command list being drawn, and
	// batches the frame's batches, each camera's in turn; both are written
	// over each frame.
	instances []instanceData
	batches   []batch
	// colorTargets is a camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// newRenderer answers a renderer that has drawn nothing, its sprite atlases'
// pages atlasPageSize texels square, holding the zero Image's drawn image,
// placed in the white texel once made.
func newRenderer(atlasPageSize uint32) *renderer {
	return &renderer{atlasPageSize: atlasPageSize, drawnImages: []drawnImage{{}}, packedImages: map[imageKey]drawnImage{}}
}

// init makes the default material, over the default shader, which it keeps
// acquired, blending premultiplied colour over what the target holds; the
// clear material, over the viewport shader, which it keeps acquired too,
// replacing what the target holds, which draws a camera's clear over its
// viewport, and what a multisampled camera that clears resolved; the
// composite material, over the viewport shader, blending as the default
// material does, which draws what a multisampled camera that does not clear
// resolved; the pack material, over the viewport shader, replacing what an
// atlas's cell holds with an image premultiplied; and the sRGB sprite
// atlas, the white texel, the zero Image, packed into it first, its cell
// written opaque white.
func (r *renderer) init(inventory render.Inventory) {
	over := render.BlendComponent{SourceFactor: render.BlendFactorOne, DestinationFactor: render.BlendFactorOneMinusSourceAlpha}
	var state render.State
	state.ColorTargets[0].Blend = render.BlendState{Color: over, Alpha: over}
	r.defaultMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "canvas default",
		Shader:           inventory.AcquireShader(defaultShaderPath),
		VertexEntryPoint: spriteVertexEntryPoint,
		State:            state,
	})
	viewportShader := inventory.AcquireShader(viewportShaderPath)
	r.clearMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas clear",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: viewportFragmentEntryPoint,
	})
	r.compositeMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas composite",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: viewportFragmentEntryPoint,
		State:              state,
	})
	r.packMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas pack",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: packFragmentEntryPoint,
	})
	r.srgbAtlas = newAtlas(inventory, "canvas sprites sRGB", render.TextureFormatRGBA8UnormSrgb, r.atlasPageSize, r.atlasPageSize, atlasGutter)
	whiteRegion, _ := r.srgbAtlas.pack(1, 1)
	cellSide := whiteRegion.Width + 2*atlasGutter
	cell := render.TextureRegion{Layer: whiteRegion.Layer, X: whiteRegion.X - atlasGutter, Y: whiteRegion.Y - atlasGutter, Width: cellSide, Height: cellSide}
	inventory.WriteTexture(r.srgbAtlas.texture, cell, slices.Repeat([]byte{255}, int(4*cellSide*cellSide)))
	r.drawnImages[0] = makeDrawnImage(r.srgbAtlas, whiteRegion)
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
// camera, and packs a sprite's image at the first frame recording it,
// whose lowest order's command list fills its cell before any camera's
// pass. Once each entry's batch key is found, it regroups state's sorted
// entries by them.
func (r *renderer) drawCameras(frame render.Frame, inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, state *drawState) {
	r.cameraDraws = r.cameraDraws[:0]
	r.mainWindow = mainWindow
	r.batches = r.batches[:0]
	r.visibleEntries = r.visibleEntries[:0]
	r.entryBounds = r.entryBounds[:0]
	r.batchKeys = r.batchKeys[:0]
	// A frame of entries always sets a camera, the default one standing in.
	if len(state.cameras) == 0 {
		return
	}
	if r.defaultMaterial == (render.Material{}) {
		r.init(inventory)
	}
	for _, entry := range state.entries {
		if entry.kind == EntryKindShape {
			shape := &state.shapes[entry.index]
			r.entryBounds = append(r.entryBounds, shape.findBounds())
			r.batchKeys = append(r.batchKeys, shape.batchKey(r.drawnImages[0].atlas))
		} else {
			sprite := &state.sprites[entry.index]
			image := r.findDrawnImage(inventory, state.logger, sprite.Image)
			r.entryBounds = append(r.entryBounds, sprite.findBounds(image.size, image.pivot))
			r.batchKeys = append(r.batchKeys, sprite.batchKey(image.atlas, state.defaultFilter))
		}
	}
	state.sorter.regroup(r.batchKeys)
	for index := range state.cameras {
		camera := &state.cameras[index]
		drawnCamera, ok := makeCameraDraw(inventory, manager, mainWindow, state.logger, camera)
		if !ok {
			continue
		}
		entriesStart := len(r.visibleEntries)
		for _, layerRun := range camera.findLayerRuns(state.sorter.entries) {
			r.visibleEntries = drawnCamera.view.appendVisibleEntries(r.visibleEntries, layerRun, r.batchKeys, r.entryBounds)
		}
		drawnCamera.entries = r.visibleEntries[entriesStart:]
		if camera.Clear || len(drawnCamera.entries) > 0 {
			r.cameraDraws = append(r.cameraDraws, drawnCamera)
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

// drawOrder draws cameraDraws, the cameras of one order in the order they
// draw, of state's entries, as one command list at that order: first the
// cells of the images packed, which the lowest order's list fills, leaving
// none for the others, then each camera's clear drawn over its viewport,
// or a multisampled
// camera's quad drawing what it resolved there, and its batches as
// instance data, in one temporary buffer, then each camera's entries'
// pass, a multisampled camera's textures acquired before it and its
// composite after it. A multisampled camera whose textures render could
// not make, which it logged, draws nothing.
func (r *renderer) drawOrder(frame render.Frame, inventory render.Inventory, cameraDraws []cameraDraw, state *drawState) {
	r.instances = r.instances[:0]
	for _, packing := range r.packings {
		// Each packing's quad, at its index, samples the image from its top
		// left gutter, clamped, so the gutter continues its edge.
		width := float32(packing.region.Width)
		height := float32(packing.region.Height)
		gutter := float32(packing.gutter)
		r.instances = append(r.instances, instanceData{region: [4]float32{-gutter / width, -gutter / height, 1 + 2*gutter/width, 1 + 2*gutter/height}})
	}
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		switch {
		case drawnCamera.camera.SampleCount == multisampledCameraSampleCount:
			// What it resolved into is drawn whole, untinted.
			resolved := drawnImage{region: [4]float32{0, 0, 1, 1}}
			drawnCamera.viewportInstance = uint32(len(r.instances))
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: m.White}, &resolved)
		case drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear:
			// A clear not by the pass's load is drawn over the viewport, whose
			// vertex stage reads only the instance's tint, region and page.
			drawnCamera.viewportInstance = uint32(len(r.instances))
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: drawnCamera.camera.ClearColor}, &r.drawnImages[0])
		}
		batchesStart := len(r.batches)
		r.batches, r.instances = appendBatches(r.batches, r.instances, drawnCamera.entries, state, r.batchKeys, r.drawnImages)
		drawnCamera.batches = r.batches[batchesStart:]
	}
	list := frame.BeginCommandList("canvas", cameraDraws[0].camera.Order)
	instances := list.AcquireTemporaryBuffer(r.instances)
	r.fillPackings(&list, inventory, instances)
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		if drawnCamera.camera.SampleCount != multisampledCameraSampleCount {
			r.drawEntries(&list, drawnCamera, instances)
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
		r.drawEntries(&list, drawnCamera, instances)
		r.drawComposite(&list, drawnCamera, instances)
	}
	list.End()
}

// drawEntries draws drawnCamera's entries, of instances, in a pass into its
// colour target, the viewport and the scissors moved from the target's
// pixels to the colour target's by its colorTargetOrigin. Its clear, when not by the
// pass's load, is drawn over the viewport first, then each batch, scissored
// to the part of the viewport the entries draw into, and a clipped batch to
// its clip's part of that, a batch whose clip holds no pixel of it not
// drawn.
func (r *renderer) drawEntries(list *render.CommandList, drawnCamera *cameraDraw, instances render.BufferRange) {
	r.colorTargets[0] = drawnCamera.colorTarget
	pass := list.BeginPass(render.PassDescriptor{Label: "canvas camera", ColorTargets: r.colorTargets[:]})
	toColorTarget := drawnCamera.colorTargetOrigin.Negate()
	viewport := drawnCamera.view.viewport.Translate(toColorTarget)
	pass.SetViewport(render.Viewport{X: float32(viewport.X), Y: float32(viewport.Y), Width: float32(viewport.Width), Height: float32(viewport.Height), MaxDepth: 1})
	pass.SetPassUniform("canvas_camera", drawnCamera.block)
	if drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear {
		r.drawInstances(&pass, r.clearMaterial, instances, drawnCamera.viewportInstance, 1, r.srgbAtlas.texture, FilterNearest)
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
		// The default material is the only one canvas makes yet.
		r.drawInstances(&pass, r.defaultMaterial, instances, drawnBatch.firstInstance, drawnBatch.instanceCount, drawnBatch.key.atlas, drawnBatch.key.filter)
	}
	pass.End()
}

// drawComposite draws what drawnCamera, a multisampled camera, resolved its
// entries into over its viewport, of instances, in a pass into its target:
// replacing what the viewport holds when the camera clears, the pass's load
// clearing the target when the viewport covers it, and blended over it
// otherwise. Its colour was encoded as the target's as its entries drew, so
// it is drawn as it is.
func (r *renderer) drawComposite(list *render.CommandList, drawnCamera *cameraDraw, instances render.BufferRange) {
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
	r.drawInstances(&pass, material, instances, drawnCamera.viewportInstance, 1, drawnCamera.colorTarget.Resolve.Texture, FilterNearest)
	pass.End()
}

// fillPackings fills the cells of the images packed, of instances, each
// packing's quad at its index, each image's source drawn premultiplied into
// its cell, its edge continued into the gutter: a pass into each page
// packed into, a draw for each cell. It
// then releases the sources, which the frame's draws still read, and
// forgets the packings. A cell of an atlas render could not make is not
// drawn.
func (r *renderer) fillPackings(list *render.CommandList, inventory render.Inventory, instances render.BufferRange) {
	for runStart := 0; runStart < len(r.packings); {
		firstPacking := &r.packings[runStart]
		runEnd := runStart + 1
		for runEnd < len(r.packings) && r.packings[runEnd].atlas == firstPacking.atlas && r.packings[runEnd].region.Layer == firstPacking.region.Layer {
			runEnd++
		}
		if firstPacking.atlas != (render.Texture{}) {
			r.colorTargets[0] = render.ColorTarget{Texture: firstPacking.atlas, Layer: firstPacking.region.Layer}
			pass := list.BeginPass(render.PassDescriptor{Label: "canvas pack", ColorTargets: r.colorTargets[:]})
			for index := runStart; index < runEnd; index++ {
				packing := &r.packings[index]
				region := packing.region
				pass.SetViewport(render.Viewport{
					X:        float32(region.X - packing.gutter),
					Y:        float32(region.Y - packing.gutter),
					Width:    float32(region.Width + 2*packing.gutter),
					Height:   float32(region.Height + 2*packing.gutter),
					MaxDepth: 1,
				})
				r.drawInstances(&pass, r.packMaterial, instances, uint32(index), 1, packing.source, FilterNearest)
			}
			pass.End()
		}
		runStart = runEnd
	}
	for _, packing := range r.packings {
		if packing.source != (render.Texture{}) {
			inventory.ReleaseTexture(packing.source)
		}
	}
	r.packings = r.packings[:0]
}

// drawInstances draws instanceCount instances of instances from
// firstInstance into pass with material, over texture, read through filter.
// Setting a material resets the draw parameters, so each
// draw sets its own.
func (r *renderer) drawInstances(pass *render.Pass, material render.Material, instances render.BufferRange, firstInstance, instanceCount uint32, texture render.Texture, filter Filter) {
	pass.SetMaterial(material)
	pass.SetBuffer("canvas_instances", instances)
	pass.SetTexture("canvas_atlas", texture)
	pass.SetSampler("canvas_sampler", filter.makeSamplerDescriptor())
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: instanceCount, FirstInstance: firstInstance})
}

// findDrawnImage answers image as canvas draws it, packing it at its first
// draw unless an image differing only in options that change no pixel was
// packed: its source acquired from render with its colour space, of the
// size render's descriptor of it gives, its file's header's, a texture
// render could not make one texel, packed into the sprite atlas of its
// colour space, or into an atlas of its own when no page holds it, its cell
// kept for the next command list to fill. It reports to logger, once for
// its path, options ImageFromFile was given for it and ignored.
func (r *renderer) findDrawnImage(inventory render.Inventory, logger *kernel.Logger, image Image) *drawnImage {
	for int(image.index) >= len(r.drawnImages) {
		r.drawnImages = append(r.drawnImages, drawnImage{})
	}
	drawn := &r.drawnImages[image.index]
	if drawn.size != (m.Vec2{}) {
		return drawn
	}
	key := imageNames.findKey(image)
	packedKey := key.makePackedKey()
	packedImage, packed := r.packedImages[packedKey]
	if !packed {
		packedImage = r.packImage(inventory, packedKey)
		r.packedImages[packedKey] = packedImage
	}
	*drawn = packedImage
	drawn.pivot = key.options.Pivot
	if key.ignoredOptions {
		ignored := Report{Kind: ReportKindImageOptionsIgnored, Err: fmt.Errorf("canvas: image %q was given more than one set of options by ImageFromFile, and is drawn with the first", key.path)}
		logger.LogErrOnce(ignored.Error(), ignored)
	}
	return drawn
}

// packImage packs the image of key, its source acquired from render with
// its colour space, of the size render's descriptor of it gives, its file's
// header's, a texture render could not make one texel, into the sprite
// atlas of its colour space, or into an atlas of its own when no page holds
// it, and answers it as drawn, of no pivot, its cell kept for the next
// command list to fill.
func (r *renderer) packImage(inventory render.Inventory, key imageKey) drawnImage {
	source := inventory.AcquireTexture(key.path, key.makeTextureOptions())
	width := uint32(1)
	height := uint32(1)
	if descriptor, held := inventory.TextureDescriptor(source); held {
		width = descriptor.Width
		height = descriptor.Height
	}
	imageAtlas := r.srgbAtlas
	if key.options.ColorSpace == ColorSpaceLinear {
		if r.linearAtlas == nil {
			r.linearAtlas = newAtlas(inventory, "canvas sprites linear", render.TextureFormatRGBA8Unorm, r.atlasPageSize, r.atlasPageSize, atlasGutter)
		}
		imageAtlas = r.linearAtlas
	}
	region, fits := imageAtlas.pack(width, height)
	if !fits {
		// An atlas of its own is exactly its size, of no gutter, so an image
		// as large as a page fills a texture of a page's size.
		imageAtlas = newAtlas(inventory, key.path, imageAtlas.format, width, height, 0)
		region, _ = imageAtlas.pack(width, height)
	}
	r.packings = append(r.packings, atlasPacking{atlas: imageAtlas.texture, region: region, gutter: imageAtlas.gutter, source: source})
	return makeDrawnImage(imageAtlas, region)
}

// atlasPacking is an image packed whose cell is still to fill: the texture
// of the atlas it is packed into, its region there, its page as the
// region's layer, the atlas's gutter its cell reaches past the region, and
// the source it is filled from.
type atlasPacking struct {
	atlas  render.Texture
	region render.TextureRegion
	gutter uint32
	source render.Texture
}
