package canvas

import (
	"cmp"
	"fmt"
	"image"
	"io/fs"
	"slices"
	"strings"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material, the clear, composite and
// pack materials and the sRGB sprite atlas, holding the white texel shapes
// sample, made at canvas's first draw; the linear sprite atlas, made at the
// first image needing it; each image drawn so far, the images packed whose cells are still to fill, and the atlases grown
// whose pages are still to copy; and what each draw reuses from the frame
// before, so a steady frame allocates nothing: the cameras drawn, the
// entries' keys, the instance data and the batches.
type renderer struct {
	// atlasPageSize is the side of a sprite atlas's pages,
	// Config.AtlasPageSize's, atlasLayerCeiling the most pages it grows to,
	// Config.AtlasLayerCeiling's, and ownTextureSize the side past which an
	// image is drawn from a texture of its own, Config.OwnTextureSize's,
	// none of them zero; mipLevelCount is how many mip levels an atlas's
	// pages have, one when Config.DoNotGenerateMipMaps is set and
	// Config.MipLevels otherwise.
	atlasPageSize     uint32
	atlasLayerCeiling uint32
	ownTextureSize    uint32
	mipLevelCount     uint32
	// defaultMaterial, clearMaterial, compositeMaterial, packMaterial and
	// srgbAtlas are zero until made, and linearAtlas until an image needs
	// it.
	defaultMaterial   render.Material
	clearMaterial     render.Material
	compositeMaterial render.Material
	packMaterial      render.Material
	srgbAtlas         *atlas
	linearAtlas       *atlas
	// drawnImages are the frames of the images drawn, each image's at its
	// Image's index, made at its first draw, then each frame at its
	// number, and one more after its sheet's last, for the frames past it;
	// the zero Image's one frame is the white texel's, and one not packed
	// is of no size. packedImages are the frames packed, by the texels
	// they are packed from, which every frame and image of the same
	// rectangle of one file and wrap shares, images differing only in their
	// pivot among them; the white texel is none of them, so nothing evicts
	// it.
	drawnImages  [][]drawnImage
	packedImages map[packedKey]*packedImage
	// decodedFiles are the files of converted images the drawing job read
	// and decoded as it packed the frame's images, each by its key, nil for
	// one that could not be, so the frames and rectangles of one file
	// decode it once, forgotten once they are packed.
	decodedFiles map[sourceKey]image.Image
	// drawnFrameCount counts the frames drawCameras drew, the frame being drawn
	// included, so a packed image last drawn in it is not evicted.
	drawnFrameCount uint64
	// packings are the images packed whose cells a command list is still
	// to fill, and growths the atlases grown whose pages it is still to
	// copy, first, both kept from the frame that made them until one does.
	packings []atlasPacking
	growths  []atlasGrowth
	// filledPages are a packing of each page a command list filled, whose
	// mips it is to generate, written over as it fills them.
	filledPages []*packedImage
	// unpackedSprites are the indexes of the frame's sprites whose images
	// were not packed as the frame began, and evictionCandidates the
	// images a full atlas may evict, in the order it evicts them, both
	// written over as they are needed.
	unpackedSprites    []uint32
	evictionCandidates []evictionCandidate
	// cameraDraws are the frame's cameras that draw, in the order they
	// draw, and mainWindow the main window they were drawn against, the
	// target of those naming none.
	cameraDraws []cameraDraw
	mainWindow  platform.Window
	// entryBounds and batchKeys are the bounds and the batch keys of the
	// frame's entries, each at its entry's index, drawsNothingEntryCount
	// how many of them draw an image's frame that draws nothing, and
	// visibleEntries the entries each camera draws, in turn, all written
	// over each frame.
	entryBounds            []m.Rect
	batchKeys              []batchKey
	drawsNothingEntryCount int
	visibleEntries         []sortedEntry
	// instances are the instance data of the command list being drawn, and
	// batches the frame's batches, each camera's in turn; both are written
	// over each frame.
	instances []instanceData
	batches   []batch
	// colorTargets is a camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// newRenderer answers a renderer that has drawn nothing, its sprite atlases'
// pages atlasPageSize texels square, growing to atlasLayerCeiling pages, an
// image wider or taller than ownTextureSize drawn from a texture of its
// own, every atlas's pages of mipLevelCount levels, holding the zero
// Image's drawn image, placed in the white texel once made.
func newRenderer(atlasPageSize, atlasLayerCeiling, ownTextureSize, mipLevelCount uint32) *renderer {
	return &renderer{
		atlasPageSize:     atlasPageSize,
		atlasLayerCeiling: atlasLayerCeiling,
		ownTextureSize:    ownTextureSize,
		mipLevelCount:     mipLevelCount,
		drawnImages:       [][]drawnImage{{{}}},
		packedImages:      map[packedKey]*packedImage{},
		decodedFiles:      map[sourceKey]image.Image{},
	}
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
// written opaque white at every mip level, as its mips would be.
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
	r.srgbAtlas = newAtlas(inventory, "canvas sprites sRGB", render.TextureFormatRGBA8UnormSrgb, r.atlasPageSize, r.atlasPageSize, r.mipLevelCount)
	whiteRegion, _ := r.srgbAtlas.pack(1, 1)
	// The cell sits on the atlas grid, so each level below halves it exactly, and
	// writes a part of the first level's texels.
	cell := r.srgbAtlas.findCell(whiteRegion)
	whiteTexels := slices.Repeat([]byte{255}, int(4*cell.Width*cell.Height))
	for mipLevel := range r.srgbAtlas.mipLevelCount {
		levelCell := render.TextureRegion{MipLevel: mipLevel, Layer: cell.Layer, X: cell.X >> mipLevel, Y: cell.Y >> mipLevel, Width: cell.Width >> mipLevel, Height: cell.Height >> mipLevel}
		inventory.WriteTexture(r.srgbAtlas.texture, levelCell, whiteTexels[:4*levelCell.Width*levelCell.Height])
	}
	r.drawnImages[0][0] = makeDrawnImage(&packedImage{atlas: r.srgbAtlas, region: whiteRegion}, ImageOptions{})
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
// entries by them. First, whether a camera is set or not, it unloads and
// preloads what state's residency requests ask. A converted image is read
// from files, decoded and converted in it.
func (r *renderer) drawCameras(frame render.Frame, inventory render.Inventory, files fs.FS, manager *platform.WindowManager, mainWindow platform.Window, state *drawState) {
	r.cameraDraws = r.cameraDraws[:0]
	r.mainWindow = mainWindow
	r.batches = r.batches[:0]
	r.visibleEntries = r.visibleEntries[:0]
	r.entryBounds = r.entryBounds[:0]
	r.batchKeys = r.batchKeys[:0]
	r.drawsNothingEntryCount = 0
	r.drawnFrameCount++
	r.applyResidencyRequests(inventory, &state.residency)
	// A frame of entries always sets a camera, the default one standing in.
	if len(state.cameras) == 0 {
		return
	}
	if r.defaultMaterial == (render.Material{}) {
		r.init(inventory)
	}
	// Every image packed is marked drawn this frame before any is packed,
	// so packing the frame's new images evicts none the frame draws.
	r.unpackedSprites = r.unpackedSprites[:0]
	for _, entry := range state.entries {
		if entry.kind == EntryKindSprite {
			if _, placed := r.findPlacedImage(&state.sprites[entry.index]); !placed {
				r.unpackedSprites = append(r.unpackedSprites, entry.index)
				continue
			}
		}
		r.appendEntryKeys(state, entry)
	}
	if len(r.unpackedSprites) > 0 {
		for _, spriteIndex := range r.unpackedSprites {
			sprite := &state.sprites[spriteIndex]
			drawn := findDrawnFrame(r.drawnImages, sprite)
			// An earlier sprite of the same frame may have placed it: packed
			// it, given it a view of its texture, or found it draws nothing.
			if drawn.packed != nil || drawn.view.Texture != (render.Texture{}) || drawn.drawsNothing {
				continue
			}
			key := imageNames.findKey(sprite.Image)
			reportIgnoredOptions(state.logger, key)
			reportRefusedConverter(state.logger, key)
			if key.ofTexture {
				placeTextureImage(inventory, state.logger, drawn, key, sprite.Frame)
				continue
			}
			packed, drawsTexels := r.packImage(inventory, files, state.logger, &state.residency, key, sprite.Frame)
			if !drawsTexels {
				drawn.drawsNothing = true
				continue
			}
			*drawn = makeDrawnImage(packed, key.options)
		}
		clear(r.decodedFiles)
		// Every key is found again once every image is packed, since
		// growing an atlas replaces its texture.
		r.entryBounds = r.entryBounds[:0]
		r.batchKeys = r.batchKeys[:0]
		r.drawsNothingEntryCount = 0
		for _, entry := range state.entries {
			r.appendEntryKeys(state, entry)
		}
	}
	state.sorter.regroup(r.batchKeys)
	if r.drawsNothingEntryCount > 0 {
		state.sorter.entries = slices.DeleteFunc(state.sorter.entries, func(entry sortedEntry) bool {
			recorded := state.entries[entry.entryIndex]
			return recorded.kind == EntryKindSprite && findDrawnFrame(r.drawnImages, &state.sprites[recorded.index]).drawsNothing
		})
	}
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
// pages of the atlases grown and the cells of the images packed, which the
// lowest order's list copies and fills, leaving none for the others, then
// each camera's clear drawn over its viewport,
// or a multisampled
// camera's quad drawing what it resolved there, and its batches as
// instance data, in one temporary buffer, then each camera's entries'
// pass, a multisampled camera's textures acquired before it and its
// composite after it. A multisampled camera whose textures render could
// not make, which it logged, draws nothing.
func (r *renderer) drawOrder(frame render.Frame, inventory render.Inventory, cameraDraws []cameraDraw, state *drawState) {
	r.instances = r.instances[:0]
	for index := range r.packings {
		// Each packing's quad is at its index.
		r.instances = append(r.instances, r.packings[index].makeInstance())
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
			r.instances = appendShapeInstances(r.instances, &Shape{Tint: drawnCamera.camera.ClearColor}, &r.drawnImages[0][0])
		}
		batchesStart := len(r.batches)
		r.batches, r.instances = appendBatches(r.batches, r.instances, drawnCamera.entries, state, r.batchKeys, r.drawnImages)
		drawnCamera.batches = r.batches[batchesStart:]
	}
	list := frame.BeginCommandList("canvas", cameraDraws[0].camera.Order)
	instances := list.AcquireTemporaryBuffer(r.instances)
	r.copyGrowths(&list, inventory)
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
		r.drawInstances(&pass, r.clearMaterial, instances, drawnCamera.viewportInstance, 1, render.TextureView{Texture: r.srgbAtlas.texture}, FilterNearest, WrapNone)
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
		r.drawInstances(&pass, r.defaultMaterial, instances, drawnBatch.firstInstance, drawnBatch.instanceCount, drawnBatch.key.texture, drawnBatch.key.filter, drawnBatch.key.wrap)
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
	r.drawInstances(&pass, material, instances, drawnCamera.viewportInstance, 1, render.TextureView{Texture: drawnCamera.colorTarget.Resolve.Texture}, FilterNearest, WrapNone)
	pass.End()
}

// copyGrowths copies the pages of each atlas grown, every mip level, into
// the texture that replaced its own, then releases the texture it had,
// which the frame's copies still read, and forgets the growths. An atlas
// render could not make a texture of copies nothing.
func (r *renderer) copyGrowths(list *render.CommandList, inventory render.Inventory) {
	for _, growth := range r.growths {
		if growth.source != (render.Texture{}) && growth.destination != (render.Texture{}) {
			for layer := range growth.layerCount {
				for mipLevel := range growth.mipLevelCount {
					region := render.TextureRegion{MipLevel: mipLevel, Layer: layer}
					list.CopyTexture(growth.source, region, growth.destination, region)
				}
			}
		}
		if growth.source != (render.Texture{}) {
			inventory.ReleaseTexture(growth.source)
		}
	}
	r.growths = r.growths[:0]
}

// fillPackings fills the cells of the images packed, of instances, each
// packing's quad at its index, each image's source drawn premultiplied into
// its cell, its edge continued into the gutter: a pass for each run of
// cells on one page, a draw for each cell, then, once every cell is
// filled, the mips of each page filled, generated once from its first
// level. It then releases the sources, which the frame's draws still read,
// and forgets the packings. A cell of an atlas render could not make is
// not drawn.
func (r *renderer) fillPackings(list *render.CommandList, inventory render.Inventory, instances render.BufferRange) {
	r.filledPages = r.filledPages[:0]
	for runStart := 0; runStart < len(r.packings); {
		firstPacked := r.packings[runStart].packed
		runEnd := runStart + 1
		for runEnd < len(r.packings) && r.packings[runEnd].packed.atlas == firstPacked.atlas && r.packings[runEnd].packed.region.Layer == firstPacked.region.Layer {
			runEnd++
		}
		if firstPacked.atlas.texture != (render.Texture{}) {
			r.colorTargets[0] = render.ColorTarget{Texture: firstPacked.atlas.texture, Layer: firstPacked.region.Layer}
			pass := list.BeginPass(render.PassDescriptor{Label: "canvas pack", ColorTargets: r.colorTargets[:]})
			for index := runStart; index < runEnd; index++ {
				packing := &r.packings[index]
				cell := packing.packed.atlas.findCell(packing.packed.region)
				pass.SetViewport(render.Viewport{X: float32(cell.X), Y: float32(cell.Y), Width: float32(cell.Width), Height: float32(cell.Height), MaxDepth: 1})
				r.drawInstances(&pass, r.packMaterial, instances, uint32(index), 1, render.TextureView{Texture: packing.source}, FilterNearest, WrapNone)
			}
			pass.End()
			filledBefore := slices.ContainsFunc(r.filledPages, func(filledPage *packedImage) bool {
				return filledPage.atlas == firstPacked.atlas && filledPage.region.Layer == firstPacked.region.Layer
			})
			if !filledBefore {
				r.filledPages = append(r.filledPages, firstPacked)
			}
		}
		runStart = runEnd
	}
	for _, filledPage := range r.filledPages {
		list.GenerateMips(filledPage.atlas.texture, filledPage.region.Layer, 1)
	}
	for _, packing := range r.packings {
		if packing.source != (render.Texture{}) {
			inventory.ReleaseTexture(packing.source)
		}
	}
	r.packings = r.packings[:0]
}

// drawInstances draws instanceCount instances of instances from
// firstInstance into pass with material, over texture, read through filter,
// repeating along the axes of wrap.
// Setting a material resets the draw parameters, so each
// draw sets its own.
func (r *renderer) drawInstances(pass *render.Pass, material render.Material, instances render.BufferRange, firstInstance, instanceCount uint32, texture render.TextureView, filter Filter, wrap Wrap) {
	pass.SetMaterial(material)
	pass.SetBuffer("canvas_instances", instances)
	pass.SetTextureView("canvas_atlas", texture)
	pass.SetSampler("canvas_sampler", filter.makeSamplerDescriptor(wrap))
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: instanceCount, FirstInstance: firstInstance})
}

// appendEntryKeys appends entry's bounds and batch key, of state's, to the
// frame's, its image placed: packed, given a view of its texture, or found
// to draw nothing, its bounds and batch key then zero, counted in
// drawsNothingEntryCount.
func (r *renderer) appendEntryKeys(state *drawState, entry recordedEntry) {
	if entry.kind == EntryKindShape {
		shape := &state.shapes[entry.index]
		r.entryBounds = append(r.entryBounds, shape.findBounds())
		r.batchKeys = append(r.batchKeys, shape.batchKey(render.TextureView{Texture: r.srgbAtlas.texture}))
		return
	}
	sprite := &state.sprites[entry.index]
	image := findDrawnFrame(r.drawnImages, sprite)
	if image.drawsNothing {
		r.entryBounds = append(r.entryBounds, m.Rect{})
		r.batchKeys = append(r.batchKeys, batchKey{})
		r.drawsNothingEntryCount++
		return
	}
	r.entryBounds = append(r.entryBounds, sprite.findBounds(image))
	r.batchKeys = append(r.batchKeys, sprite.batchKey(image, state.defaultFilter))
}

// findPlacedImage answers sprite's image's frame as drawn, and whether it
// was placed when drawn before: packed, given a view of its texture, or
// found to draw nothing, its image's drawn frames made at its first draw. A packed frame is marked
// drawn this frame, so no atlas evicts it in it.
func (r *renderer) findPlacedImage(sprite *Sprite) (*drawnImage, bool) {
	image := sprite.Image
	for int(image.index) >= len(r.drawnImages) {
		r.drawnImages = append(r.drawnImages, nil)
	}
	if r.drawnImages[image.index] == nil {
		sheet := imageNames.findKey(image).options.Sheet
		r.drawnImages[image.index] = make([]drawnImage, cmp.Or(sheet.Columns, 1)*cmp.Or(sheet.Rows, 1)+1)
	}
	drawn := findDrawnFrame(r.drawnImages, sprite)
	if drawn.packed != nil {
		drawn.packed.lastDrawnFrame = r.drawnFrameCount
	}
	return drawn, drawn.packed != nil || drawn.view.Texture != (render.Texture{}) || drawn.drawsNothing
}

// packImage packs frame of the image of key, marked drawn this frame, and
// answers where, and whether it draws texels: false for a frame whose
// rectangle does not lie within its file, which is not packed and is
// reported to logger once for its path, and for a converted frame whose
// converter answered no texel. A converted image's file is read from files
// and decoded, as decodeConvertedFile does; any other's, or one that could
// not be decoded, which render draws magenta, is acquired from render with
// its colour space, sharing the texture Assets.Preload acquired, which it
// then releases from requests, of the size render's descriptor of it
// gives, its file's header's, a texture render could not make one texel.
// A frame of the texels of one packed already, of the same rectangle of
// the file, converter and wrap, shares its cell, its source released. Any
// other is packed into the sprite atlas of its colour space, a converted
// one as a texture made of its converter's output, placed by its bounds
// within the frame, grown or evicting images when full, or into an atlas
// of its own, exactly its size with no gutter, when it is wider or taller
// than ownTextureSize, when no page holds its cell, or when the atlas is
// at its ceiling with nothing to evict, reported to logger, its cell kept
// for the next command list to fill.
func (r *renderer) packImage(inventory render.Inventory, files fs.FS, logger *kernel.Logger, requests *residencyRequestQueue, key imageKey, frame uint32) (*packedImage, bool) {
	sourceKey := sourceKey{path: key.path, colorSpace: key.options.ColorSpace}
	converter := key.options.Convert
	var picture image.Image
	if converter != nil {
		picture = r.decodeConvertedFile(files, requests, sourceKey)
		if picture == nil {
			converter = nil
		}
	}
	var source render.Texture
	fileWidth := 1
	fileHeight := 1
	if picture != nil {
		fileWidth = picture.Bounds().Dx()
		fileHeight = picture.Bounds().Dy()
	} else {
		source = inventory.AcquireTexture(sourceKey.path, sourceKey.makeTextureOptions())
		requests.releasePreload(inventory, sourceKey)
		if descriptor, held := inventory.TextureDescriptor(source); held {
			fileWidth = int(descriptor.Width)
			fileHeight = int(descriptor.Height)
		}
	}
	frameRect, withinFile := key.options.findFrameRect(fileWidth, fileHeight, frame)
	if !withinFile {
		reportFrameOutOfBounds(logger, key, frame, "file", fileWidth, fileHeight)
	}
	packedKey := packedKey{source: sourceKey, rect: frameRect, converter: converter, wrap: key.options.Wrap}
	packed, shared := r.packedImages[packedKey]
	if shared || !withinFile {
		if source != (render.Texture{}) {
			inventory.ReleaseTexture(source)
		}
		if shared {
			packed.lastDrawnFrame = r.drawnFrameCount
		}
		return packed, withinFile
	}
	spriteAtlas := r.srgbAtlas
	if key.options.ColorSpace == ColorSpaceLinear {
		if r.linearAtlas == nil {
			r.linearAtlas = newAtlas(inventory, "canvas sprites linear", render.TextureFormatRGBA8Unorm, r.atlasPageSize, r.atlasPageSize, r.mipLevelCount)
		}
		spriteAtlas = r.linearAtlas
	}
	packed = &packedImage{atlas: spriteAtlas, lastDrawnFrame: r.drawnFrameCount}
	sourceRect := frameRect
	if converter != nil {
		output := convertFrame(picture, frameRect, converter)
		if output.Rect.Empty() {
			return nil, false
		}
		sourceRect = m.Recti{Width: output.Rect.Dx(), Height: output.Rect.Dy()}
		source = inventory.MakeTexture(render.TextureDescriptor{Label: key.path, Width: uint32(sourceRect.Width), Height: uint32(sourceRect.Height), Format: spriteAtlas.format}, output.Pix)
		packed.frameBounds = m.Recti{X: -output.Rect.Min.X, Y: -output.Rect.Min.Y, Width: frameRect.Width, Height: frameRect.Height}.Float()
	}
	width := uint32(sourceRect.Width)
	height := uint32(sourceRect.Height)
	fits := false
	cellWidth, cellHeight := spriteAtlas.findCellSize(width, height)
	holdsCell := cellWidth <= r.atlasPageSize && cellHeight <= r.atlasPageSize
	if width <= r.ownTextureSize && height <= r.ownTextureSize && holdsCell {
		packed.region, fits = r.packIntoSpriteAtlas(inventory, logger, spriteAtlas, key.path, width, height)
	}
	if !fits {
		// An atlas of its own is exactly its size, of no gutter, so an image
		// as large as a page fills a texture of a page's size.
		packed.atlas, packed.region = newOwnAtlas(inventory, key.path, spriteAtlas.format, width, height, spriteAtlas.mipLevelCount)
	}
	r.packedImages[packedKey] = packed
	r.packings = append(r.packings, atlasPacking{packed: packed, source: source, frameRect: sourceRect, wrap: key.options.Wrap})
	return packed, true
}

// decodeConvertedFile answers the file of key, a converted image's, read
// from files and decoded, once for each drawing job, taking the picture
// Assets.Preload decoded from requests, or nil when it cannot be decoded,
// as decodeImageFile answers it.
func (r *renderer) decodeConvertedFile(files fs.FS, requests *residencyRequestQueue, key sourceKey) image.Image {
	if picture, decoded := r.decodedFiles[key]; decoded {
		return picture
	}
	picture := requests.takePreloadedFile(key)
	if picture == nil {
		picture = decodeImageFile(files, key.path)
	}
	r.decodedFiles[key] = picture
	return picture
}

// packIntoSpriteAtlas packs an image of width by height texels, at path,
// whose cell a page of spriteAtlas holds, into spriteAtlas, and answers
// where, or false when it does not fit: on the first page holding it; else
// on a new page, the atlas doubling its pages while under its ceiling, a
// growth not yet copied merged into one copy of the pages it had; else
// where the images it evicts leave room, the least lately drawn first,
// never one drawn this frame. An atlas at its ceiling with nothing to evict
// is reported to logger, once for the atlas.
func (r *renderer) packIntoSpriteAtlas(inventory render.Inventory, logger *kernel.Logger, spriteAtlas *atlas, path string, width, height uint32) (render.TextureRegion, bool) {
	if region, fits := spriteAtlas.pack(width, height); fits {
		return region, true
	}
	if uint32(len(spriteAtlas.pages)) < r.atlasLayerCeiling {
		growth := spriteAtlas.grow(inventory, r.atlasLayerCeiling)
		// A texture grown into whose copy is still to record holds nothing
		// that copy would not write, so it is copied past and released.
		pendingGrowth := slices.IndexFunc(r.growths, func(pending atlasGrowth) bool { return pending.destination == growth.source })
		if pendingGrowth >= 0 && growth.source != (render.Texture{}) {
			inventory.ReleaseTexture(growth.source)
			r.growths[pendingGrowth].destination = growth.destination
		} else {
			r.growths = append(r.growths, growth)
		}
		return spriteAtlas.pack(width, height)
	}
	r.evictionCandidates = r.evictionCandidates[:0]
	for key, packed := range r.packedImages {
		if packed.atlas == spriteAtlas && packed.lastDrawnFrame != r.drawnFrameCount {
			r.evictionCandidates = append(r.evictionCandidates, evictionCandidate{key: key, packed: packed})
		}
	}
	// Those last drawn in one frame go in their paths' order, then their
	// rectangles' rows and columns.
	slices.SortFunc(r.evictionCandidates, func(a, b evictionCandidate) int {
		return cmp.Or(cmp.Compare(a.packed.lastDrawnFrame, b.packed.lastDrawnFrame), strings.Compare(a.key.source.path, b.key.source.path), cmp.Compare(a.key.rect.Y, b.key.rect.Y), cmp.Compare(a.key.rect.X, b.key.rect.X))
	})
	for _, candidate := range r.evictionCandidates {
		r.dropPackedImage(inventory, candidate.key, candidate.packed)
		if region, fits := spriteAtlas.pack(width, height); fits {
			return region, true
		}
	}
	fullReport := Report{Kind: ReportKindAtlasFull, Err: fmt.Errorf("canvas: the sprite atlas %q is full at its ceiling of %d pages, with nothing to evict that was not drawn this frame, so image %q is drawn from a texture of its own", spriteAtlas.label, len(spriteAtlas.pages), path)}
	logger.LogErrOnce(atlasFullLogKey+spriteAtlas.label, fullReport)
	return render.TextureRegion{}, false
}

// applyResidencyRequests drops each frame packed of the paths requests asks
// to unload, in the order asked, so its next draw loads it again, and
// forgets each frame of theirs found outside its file, so its next draw
// looks again; an image of a render texture has no path.
func (r *renderer) applyResidencyRequests(inventory render.Inventory, requests *residencyRequestQueue) {
	requests.mutex.Lock()
	defer requests.mutex.Unlock()
	for _, unloaded := range requests.unloadedPaths {
		for key, packed := range r.packedImages {
			if isUnder(key.source.path, unloaded) {
				r.dropPackedImage(inventory, key, packed)
			}
		}
		for index, frames := range r.drawnImages {
			if !slices.ContainsFunc(frames, func(drawn drawnImage) bool { return drawn.drawsNothing }) {
				continue
			}
			if key := imageNames.findKey(Image{index: uint32(index)}); !key.ofTexture && isUnder(key.path, unloaded) {
				for frame := range frames {
					frames[frame].drawsNothing = false
				}
			}
		}
	}
	requests.unloadedPaths = requests.unloadedPaths[:0]
}

// dropPackedImage forgets packed, the frame of key packed: freed from its
// atlas, its own atlas's texture released through inventory; a cell still
// to fill forgotten, its source released; and each frame drawn of it left
// unpacked, so its next draw packs it again.
func (r *renderer) dropPackedImage(inventory render.Inventory, key packedKey, packed *packedImage) {
	delete(r.packedImages, key)
	packed.atlas.free(inventory, packed.region)
	for index, packing := range r.packings {
		if packing.packed == packed {
			if packing.source != (render.Texture{}) {
				inventory.ReleaseTexture(packing.source)
			}
			r.packings = slices.Delete(r.packings, index, index+1)
			break
		}
	}
	for _, frames := range r.drawnImages {
		for index := range frames {
			if frames[index].packed == packed {
				frames[index] = drawnImage{}
			}
		}
	}
}

// findDrawnFrame answers sprite's image's frame as drawn, of drawnImages, a
// renderer's, which hold its image's frames: its Frame's, or, past its
// sheet's last, the one after the last, and the zero Image's one frame,
// the white texel, whatever its Frame.
func findDrawnFrame(drawnImages [][]drawnImage, sprite *Sprite) *drawnImage {
	frames := drawnImages[sprite.Image.index]
	if len(frames) == 0 {
		panic("canvas: finding a sprite's frame of an image whose drawn frames were never made")
	}
	return &frames[min(sprite.Frame, uint32(len(frames)-1))]
}

// evictionCandidate is a frame packed that a full atlas may evict: its key,
// the texels it holds, and where it is packed.
type evictionCandidate struct {
	key    packedKey
	packed *packedImage
}
