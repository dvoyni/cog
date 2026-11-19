package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material, the clear material and
// the white texel shapes sample, made at canvas's first draw, and what each
// draw reuses from the frame before, so a steady frame allocates nothing:
// the cameras drawn, the instance data and the batches.
type renderer struct {
	// defaultMaterial, clearMaterial and whiteTexel are zero until made.
	defaultMaterial render.Material
	clearMaterial   render.Material
	whiteTexel      render.Texture
	// cameraDraws are the frame's cameras that draw, in the order they
	// draw, and mainWindow the main window they were drawn against, the
	// target of those naming none.
	cameraDraws []cameraDraw
	mainWindow  platform.Window
	// shapeBounds are the bounds of the frame's shapes, each at its shape's
	// index, and visibleEntries the entries each camera draws, in turn,
	// both written over each frame.
	shapeBounds    []m.Rect
	visibleEntries []sortedEntry
	// instances are the instance data of the command list being drawn, and
	// batches the frame's batches, each camera's in turn; both are written
	// over each frame.
	instances []instanceData
	batches   []batch
	// colorTargets is a camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// init makes the default material, over the default shader, which it keeps
// acquired, blending premultiplied colour over what the target holds; the
// clear material, over the same shader through its viewport vertex stage,
// replacing what the target holds;
// and the white texel, a 1×1 layer of opaque white.
func (r *renderer) init(inventory render.Inventory) {
	shader := inventory.AcquireShader(defaultShaderPath)
	over := render.BlendComponent{SourceFactor: render.BlendFactorOne, DestinationFactor: render.BlendFactorOneMinusSourceAlpha}
	var state render.State
	state.ColorTargets[0].Blend = render.BlendState{Color: over, Alpha: over}
	r.defaultMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "canvas default",
		Shader:           shader,
		VertexEntryPoint: spriteVertexEntryPoint,
		State:            state,
	})
	r.clearMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "canvas clear",
		Shader:           shader,
		VertexEntryPoint: viewportVertexEntryPoint,
	})
	r.whiteTexel = inventory.MakeTexture(render.TextureDescriptor{Label: "canvas white texel", Width: 1, Height: 1, Format: render.TextureFormatRGBA8Unorm}, []byte{255, 255, 255, 255})
}

// drawCameras draws the entries state sorted through each of its cameras,
// mainWindow the target of those naming no other: one command list for each
// distinct order, its cameras' passes in the order they draw, each a pass
// into its target over its viewport, cleared there when it clears, then an
// instanced draw of each batch of the entries its layers hold over the white
// texel, each batch scissored to its clip. A camera culls the entries
// whose bounds miss its view, or their clip's part of it; one drawing no
// entry that does not clear draws nothing, nor does one makeCameraDraw finds
// draws nothing. It makes the materials
// and the texel at the first draw.
func (r *renderer) drawCameras(frame render.Frame, inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, state *drawState) {
	r.cameraDraws = r.cameraDraws[:0]
	r.mainWindow = mainWindow
	r.batches = r.batches[:0]
	r.visibleEntries = r.visibleEntries[:0]
	r.shapeBounds = r.shapeBounds[:0]
	for index := range state.shapes {
		r.shapeBounds = append(r.shapeBounds, state.shapes[index].findBounds())
	}
	for index := range state.cameras {
		camera := &state.cameras[index]
		drawnCamera, ok := makeCameraDraw(inventory, manager, mainWindow, state.logger, camera)
		if !ok {
			continue
		}
		entriesStart := len(r.visibleEntries)
		for _, layerRun := range camera.findLayerRuns(state.sorter.entries) {
			r.visibleEntries = drawnCamera.view.appendVisibleEntries(r.visibleEntries, layerRun, state.shapes, r.shapeBounds)
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
		r.drawOrder(frame, r.cameraDraws[orderStart:orderEnd], state.shapes)
		orderStart = orderEnd
	}
}

// drawOrder draws cameraDraws, the cameras of one order in the order they
// draw, of shapes, as one command list at that order: first each camera's
// clear drawn over its viewport and its batches as instance data, in one
// temporary buffer, then each camera's pass, its entries scissored to the
// part of its viewport they draw into, and a clipped batch's to its clip's
// part of that, a batch whose clip holds no pixel of it not drawn.
func (r *renderer) drawOrder(frame render.Frame, cameraDraws []cameraDraw, shapes []Shape) {
	r.instances = r.instances[:0]
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		// A clear not by the pass's load is drawn over the viewport.
		if drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear {
			// The viewport vertex stage reads only the instance's tint and
			// region.
			drawnCamera.clearInstance = uint32(len(r.instances))
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: drawnCamera.camera.ClearColor})
		}
		batchesStart := len(r.batches)
		r.batches, r.instances = appendBatches(r.batches, r.instances, drawnCamera.entries, shapes)
		drawnCamera.batches = r.batches[batchesStart:]
	}
	list := frame.BeginCommandList("canvas", cameraDraws[0].camera.Order)
	instances := list.AcquireTemporaryBuffer(r.instances)
	for index := range cameraDraws {
		drawnCamera := &cameraDraws[index]
		r.colorTargets[0] = drawnCamera.colorTarget
		pass := list.BeginPass(render.PassDescriptor{Label: "canvas camera", ColorTargets: r.colorTargets[:]})
		viewport := drawnCamera.view.viewport
		pass.SetViewport(render.Viewport{X: float32(viewport.X), Y: float32(viewport.Y), Width: float32(viewport.Width), Height: float32(viewport.Height), MaxDepth: 1})
		pass.SetPassUniform("canvas_camera", drawnCamera.block)
		if drawnCamera.camera.Clear && !drawnCamera.colorTarget.Clear {
			r.drawInstances(&pass, r.clearMaterial, instances, drawnCamera.clearInstance, 1)
		}
		for _, drawnBatch := range drawnCamera.batches {
			// The entries draw between FitInscribe's bars, which the clear
			// covered, and within their clip.
			scissor, clipDrawn := drawnCamera.view.placeClip(drawnBatch.key.clip)
			if !clipDrawn {
				continue
			}
			pass.SetScissorRectangle(render.ScissorRectangle{X: uint32(scissor.X), Y: uint32(scissor.Y), Width: uint32(scissor.Width), Height: uint32(scissor.Height)})
			// The default material is the only one canvas makes yet.
			r.drawInstances(&pass, r.defaultMaterial, instances, drawnBatch.firstInstance, drawnBatch.instanceCount)
		}
		pass.End()
	}
	list.End()
}

// drawInstances draws instanceCount instances of instances from
// firstInstance into pass with material, over the white texel. Setting a
// material resets the draw parameters, so each draw sets its own.
func (r *renderer) drawInstances(pass *render.Pass, material render.Material, instances render.BufferRange, firstInstance, instanceCount uint32) {
	pass.SetMaterial(material)
	pass.SetBuffer("canvas_instances", instances)
	pass.SetTexture("canvas_atlas", r.whiteTexel)
	pass.SetSampler("canvas_sampler", render.SamplerDescriptor{})
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: instanceCount, FirstInstance: firstInstance})
}
