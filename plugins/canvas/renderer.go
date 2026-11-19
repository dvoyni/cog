package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material, the clear and composite
// materials and the white texel shapes sample, made at canvas's first draw;
// each image drawn so far; and what each draw reuses from the frame before,
// so a steady frame allocates nothing: the cameras drawn, the instance data
// and the batches.
type renderer struct {
	// defaultMaterial, clearMaterial, compositeMaterial and whiteTexel are
	// zero until made.
	defaultMaterial   render.Material
	clearMaterial     render.Material
	compositeMaterial render.Material
	whiteTexel        render.Texture
	// drawnImages are the images drawn, each at its Image's index, the
	// zero Image's the white texel's, and one not yet drawn of no size.
	drawnImages []drawnImage
	// cameraDraws are the frame's cameras that draw, in the order they
	// draw, and mainWindow the main window they were drawn against, the
	// target of those naming none.
	cameraDraws []cameraDraw
	mainWindow  platform.Window
	// entryBounds are the bounds of the frame's entries, each at its
	// entry's index, and visibleEntries the entries each camera draws, in
	// turn, both written over each frame.
	entryBounds    []m.Rect
	visibleEntries []sortedEntry
	// instances are the instance data of the command list being drawn, and
	// batches the frame's batches, each camera's in turn; both are written
	// over each frame.
	instances []instanceData
	batches   []batch
	// colorTargets is a camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// newRenderer answers a renderer that has drawn nothing, holding the zero
// Image's drawn image, one texel, centred, its texture the white texel
// once made.
func newRenderer() *renderer {
	return &renderer{drawnImages: []drawnImage{{size: m.Vec2{X: 1, Y: 1}}}}
}

// init makes the default material, over the default shader, which it keeps
// acquired, blending premultiplied colour over what the target holds; the
// clear material, over the viewport shader, which it keeps acquired too,
// replacing what the target holds, which draws a camera's clear over its
// viewport, and what a multisampled camera that clears resolved; the
// composite material, over the viewport shader, blending as the default
// material does, which draws what a multisampled camera that does not clear
// resolved; and the white texel, a 1×1 layer of opaque white, the zero
// Image's texture.
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
		Label:            "canvas clear",
		Shader:           viewportShader,
		VertexEntryPoint: viewportVertexEntryPoint,
	})
	r.compositeMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "canvas composite",
		Shader:           viewportShader,
		VertexEntryPoint: viewportVertexEntryPoint,
		State:            state,
	})
	r.whiteTexel = inventory.MakeTexture(render.TextureDescriptor{Label: "canvas white texel", Width: 1, Height: 1, Format: render.TextureFormatRGBA8Unorm}, []byte{255, 255, 255, 255})
	r.drawnImages[0].texture = r.whiteTexel
}

// drawCameras draws the entries state sorted through each of its cameras,
// mainWindow the target of those naming no other: one command list for each
// distinct order, its cameras' passes in the order they draw, each a pass
// into its target over its viewport, cleared there when it clears, then an
// instanced draw of each batch of the entries its layers hold over the white
// texel or its sprite's image, each batch scissored to its clip, a
// multisampled camera's into a texture of its viewport then resolved and
// drawn over its viewport. A camera culls the entries whose bounds miss its
// view, or their clip's part of it; one drawing no entry that does not
// clear draws nothing, nor does one makeCameraDraw finds draws nothing. It
// makes the materials and the texel at the first draw, and acquires a
// sprite's image from render at the first frame recording it.
func (r *renderer) drawCameras(frame render.Frame, inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, state *drawState) {
	r.cameraDraws = r.cameraDraws[:0]
	r.mainWindow = mainWindow
	r.batches = r.batches[:0]
	r.visibleEntries = r.visibleEntries[:0]
	r.entryBounds = r.entryBounds[:0]
	for _, entry := range state.entries {
		if entry.kind == EntryKindShape {
			r.entryBounds = append(r.entryBounds, state.shapes[entry.index].findBounds())
		} else {
			sprite := &state.sprites[entry.index]
			image := r.findDrawnImage(inventory, state.logger, sprite.Image)
			r.entryBounds = append(r.entryBounds, sprite.findBounds(image.size, image.pivot))
		}
	}
	for index := range state.cameras {
		camera := &state.cameras[index]
		drawnCamera, ok := makeCameraDraw(inventory, manager, mainWindow, state.logger, camera)
		if !ok {
			continue
		}
		entriesStart := len(r.visibleEntries)
		for _, layerRun := range camera.findLayerRuns(state.sorter.entries) {
			r.visibleEntries = drawnCamera.view.appendVisibleEntries(r.visibleEntries, layerRun, state.batchKeys, r.entryBounds)
		}
		drawnCamera.entries = r.visibleEntries[entriesStart:]
		if camera.Clear || len(drawnCamera.entries) > 0 {
			r.cameraDraws = append(r.cameraDraws, drawnCamera)
		}
	}
	if len(r.cameraDraws) == 0 {
		return
	}
	if r.defaultMaterial == (render.Material{}) {
		r.init(inventory)
	}
	for orderStart := 0; orderStart < len(r.cameraDraws); {
		orderEnd := orderStart + 1
		for orderEnd < len(r.cameraDraws) && r.cameraDraws[orderEnd].camera.Order == r.cameraDraws[orderStart].camera.Order {
			orderEnd++
		}
		r.drawOrder(frame, r.cameraDraws[orderStart:orderEnd], state)
		orderStart = orderEnd
	}
}

// drawOrder draws cameraDraws, the cameras of one order in the order they
// draw, of state's entries, as one command list at that order: first each camera's
// clear drawn over its viewport, or a multisampled camera's quad drawing
// what it resolved there, and its batches as instance data, in one
// temporary buffer, then each camera's entries' pass, a multisampled
// camera's textures acquired before it and its composite after it. A
// multisampled camera whose textures render could not make, which it
// logged, draws nothing.
func (r *renderer) drawOrder(frame render.Frame, cameraDraws []cameraDraw, state *drawState) {
	r.instances = r.instances[:0]
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		switch {
		case drawnCamera.camera.SampleCount == multisampledCameraSampleCount:
			// What it resolved into is drawn whole, untinted.
			drawnCamera.viewportInstance = uint32(len(r.instances))
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: m.White})
		case drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear:
			// A clear not by the pass's load is drawn over the viewport, whose
			// vertex stage reads only the instance's tint and region.
			drawnCamera.viewportInstance = uint32(len(r.instances))
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: drawnCamera.camera.ClearColor})
		}
		batchesStart := len(r.batches)
		r.batches, r.instances = appendBatches(r.batches, r.instances, drawnCamera.entries, state, r.drawnImages)
		drawnCamera.batches = r.batches[batchesStart:]
	}
	list := frame.BeginCommandList("canvas", cameraDraws[0].camera.Order)
	instances := list.AcquireTemporaryBuffer(r.instances)
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
		r.drawInstances(&pass, r.clearMaterial, instances, drawnCamera.viewportInstance, 1, r.whiteTexel, FilterNearest)
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
		r.drawInstances(&pass, r.defaultMaterial, instances, drawnBatch.firstInstance, drawnBatch.instanceCount, r.drawnImages[drawnBatch.key.image.index].texture, drawnBatch.key.filter)
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

// findDrawnImage answers image as canvas draws it, acquiring its texture
// from render with its colour space at its first draw, sized from render's
// descriptor of it, its file's header's size, a texture render could not
// make one texel, and reporting to logger, once for its path, options
// ImageFromFile was given for it and ignored.
func (r *renderer) findDrawnImage(inventory render.Inventory, logger *kernel.Logger, image Image) *drawnImage {
	for int(image.index) >= len(r.drawnImages) {
		r.drawnImages = append(r.drawnImages, drawnImage{})
	}
	drawn := &r.drawnImages[image.index]
	if drawn.size != (m.Vec2{}) {
		return drawn
	}
	key := imageNames.findKey(image)
	drawn.pivot = key.options.Pivot
	drawn.texture = inventory.AcquireTexture(key.path, key.makeTextureOptions())
	drawn.size = m.Vec2{X: 1, Y: 1}
	if descriptor, held := inventory.TextureDescriptor(drawn.texture); held {
		drawn.size = m.Vec2{X: float32(descriptor.Width), Y: float32(descriptor.Height)}
	}
	if key.ignoredOptions {
		ignored := Report{Kind: ReportKindImageOptionsIgnored, Err: fmt.Errorf("canvas: image %q was given more than one set of options by ImageFromFile, and is drawn with the first", key.path)}
		logger.LogErrOnce(ignored.Error(), ignored)
	}
	return drawn
}

// drawnImage is an image as canvas draws it: the texture it is drawn from,
// its size in texels and its pivot, once acquired from render at its first
// draw, and of no size until then.
type drawnImage struct {
	texture render.Texture
	size    m.Vec2
	pivot   Pivot
}
