package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// Assets is the task parameter a task reaches canvas's assets and tools
// through, declared in its signature as kernel.Read is:
//
//	func inspectOnKey(k *kernel.Kernel, event canvas.FrameEvent, assets canvas.Assets) kernel.NoResult
//
// It locks canvas's draw state and platform's FS for read, and, through
// the render.Frame and render.Inventory it holds, render's frame state, its
// asset table and gfx's Device, all for read, so tasks holding it run
// together, and with recording tasks. It is valid only while the task's job
// runs.
type Assets struct {
	state     kernel.Read[*drawState]
	files     kernel.Read[platform.FS]
	frame     render.Frame
	inventory render.Inventory
}

// Declare declares Assets' locks and answers it bound to what they guard.
func (Assets) Declare(access *kernel.Access) Assets {
	return Assets{
		state:     access.Read[*drawState](),
		files:     access.Read[platform.FS](),
		frame:     render.Frame{}.Declare(access),
		inventory: render.Inventory{}.Declare(access),
	}
}

// Preload loads image ahead of its first draw, so the frame drawing it
// first does not decode it: its file is acquired from render now, which
// decodes it before the frame's submit, or the next frame's once this
// frame's recording has ended, and canvas packs it as it is first drawn. A
// converted image's file is read through platform's FS and decoded in the
// caller's job, and converted as it is first drawn. An image packed
// already, or preloaded and not yet drawn, is not loaded again, nor is the
// zero Image, the white texel, nor an image of a render texture,
// ImageFromTexture's, which loads nothing.
func (a Assets) Preload(image Image) {
	if image == (Image{}) {
		return
	}
	key := imageNames.findKey(image)
	if key.ofTexture {
		return
	}
	state := a.state.Get()
	source := sourceKey{path: key.path, colorSpace: key.options.ColorSpace}
	if key.options.Convert != nil {
		state.residency.requestFilePreload(a.files.Get(), source)
		return
	}
	state.residency.requestPreload(a.inventory, source)
}

// Unload unloads the images of path, or, when path ends in a slash, of
// every path in the folder it names, so Unload("level1/") unloads a
// level's art and not level10's: their regions of the sprite atlases are
// freed and their own textures released as canvas next draws, and a later
// draw loads them again. Their sizes ImageSize read are forgotten, and the
// reports canvas made of them once may be made again. Its mesh files are
// unloaded too, their buffers and meshes released as canvas next draws, so
// a game changing one puts its file again and unloads its path, and the next
// draw loads the new one; one that failed to load is loaded again at its
// next draw, and reported again if it fails.
func (a Assets) Unload(path string) {
	state := a.state.Get()
	state.residency.requestUnload(a.inventory, path)
	state.imageSizes.forget(state.reporter, path)
	for _, unloaded := range imageNames.findPaths(path, func(key imageKey) (string, bool) { return key.path, !key.ofTexture }) {
		state.reporter.forget(imageOptionsIgnoredLogKey + unloaded)
		state.reporter.forget(frameOutOfBoundsLogKey + unloaded)
		state.reporter.forget(converterRefusedLogKey + unloaded)
	}
	for _, unloaded := range meshFileNames.findPaths(path, func(path string) (string, bool) { return path, true }) {
		state.reporter.forget(meshFileNotLoadedLogKey + unloaded)
	}
}

// ImageSize answers image's size in texels before it is drawn, its
// SourceRect's, or its sheet's frames', a converted image's before it is
// converted, read from its file's header alone,
// through platform's FS, and kept for its path until it is unloaded, and
// whether the header was read: false for a file that is missing or no
// image render loads, reported once for its path as
// ReportKindImageHeaderUnread, and for an image whose first frame does not
// lie within its file, which draws nothing. An image of a render texture,
// ImageFromTexture's, is measured on render's descriptor of the texture,
// false for one render does not hold. The zero Image, the white texel, is
// one texel.
func (a Assets) ImageSize(image Image) (m.Vec2, bool) {
	if image == (Image{}) {
		return m.Vec2{X: 1, Y: 1}, true
	}
	state := a.state.Get()
	key := imageNames.findKey(image)
	var size m.Vec2
	var read bool
	if key.ofTexture {
		var descriptor render.TextureDescriptor
		descriptor, read = a.inventory.TextureDescriptor(key.texture)
		size = m.Vec2{X: float32(descriptor.Width), Y: float32(descriptor.Height)}
	} else {
		size, read = state.imageSizes.find(a.files.Get(), state.reporter, key.path)
	}
	if !read {
		return m.Vec2{}, false
	}
	frameRect, withinFile := key.options.findFrameRect(int(size.X), int(size.Y), 0)
	return frameRect.Size().Float(), withinFile
}

// MakeMaterial makes a 2D material of descriptor and answers it: canvas
// acquires its shader and makes the render materials underneath, which its
// user never holds, one of its vertex stage and one of canvas's mesh vertex
// stage, for meshes of canvas.Vertex, its parameters set on both, blending as descriptor's Blend says, with no depth test,
// since the sort key orders entries, and no culling. A shader render cannot
// load or compile, or entry points it lacks, are reported by render, and
// the material's entries are skipped. A material taking an argument has its
// shader preprocessed with the define CANVAS_ARGUMENT, and the argument's
// layout checked against the shader's struct Argument: a mismatch is
// reported once for the shader and the type as
// ReportKindArgumentLayoutMismatch, and the material's entries are skipped.
// Its maker gives it back once, with ReleaseMaterial.
func (a Assets) MakeMaterial(descriptor MaterialDescriptor) Material {
	state := a.state.Get()
	return state.materials.make(a.inventory, state.reporter, descriptor)
}

// ReleaseMaterial gives back material, made by MakeMaterial: the frame
// recording as it is released still draws its entries, and once canvas has
// drawn that frame, it releases the render materials and the shader, and
// material names nothing, its later entries skipped. A material canvas does
// not hold, released already, or the default material, the zero Material,
// is refused, reported once as ReportKindMaterialNotHeld.
func (a Assets) ReleaseMaterial(material Material) {
	state := a.state.Get()
	if !state.materials.release(material) {
		reportMaterialNotHeld(state.reporter, material, "releasing")
	}
}

// SetMaterialTexture binds texture whole to material's binding named name, a
// texture of its shader's @group(1), as render's Inventory.SetMaterialTexture
// does, refusing and reporting what it refuses.
// A draw uses material's parameters as they stand when canvas draws the
// frame, after every task hung off FrameEvent, so a change made by one of
// them shows in that frame's entries, whether made before or after it
// records them; a change made by a task the kernel does not order before
// canvas's drawing may show in this frame or only in the next.
// A material canvas does not hold, or the default material, the zero
// Material, whose parameters are canvas's, is refused, reported once as
// ReportKindMaterialNotHeld.
func (a Assets) SetMaterialTexture(material Material, name string, texture render.Texture) {
	state := a.state.Get()
	renderMaterials, held := state.materials.findHeld(material)
	if !held {
		reportMaterialNotHeld(state.reporter, material, "setting a texture of")
		return
	}
	for _, renderMaterial := range renderMaterials {
		a.inventory.SetMaterialTexture(renderMaterial, name, texture)
	}
}

// SetMaterialUniform binds value, plain data with no pointers mirroring a
// WGSL struct, to material's uniform block named name, of its shader's
// @group(1), such as the time a swaying material animates by, set from
// whichever clock owns it, as render's Inventory.SetMaterialUniform does,
// checking value's layout and refusing and reporting what it refuses.
// A draw uses material's parameters as they stand when canvas draws the
// frame, after every task hung off FrameEvent, so a change made by one of
// them shows in that frame's entries, whether made before or after it
// records them; a change made by a task the kernel does not order before
// canvas's drawing may show in this frame or only in the next.
// A material canvas does not hold, or the default material, the zero
// Material, whose parameters are canvas's, is refused, reported once as
// ReportKindMaterialNotHeld.
func (a Assets) SetMaterialUniform[T any](material Material, name string, value T) {
	state := a.state.Get()
	renderMaterials, held := state.materials.findHeld(material)
	if !held {
		reportMaterialNotHeld(state.reporter, material, "setting a uniform of")
		return
	}
	for _, renderMaterial := range renderMaterials {
		a.inventory.SetMaterialUniform(renderMaterial, name, value)
	}
}

// RequestInspection asks for an inspection of the first frame whose
// FrameEvent is published after the call, and asks render for its own
// inspection of that frame, one at a time: a request while one is pending,
// asked for and not yet delivered, joins it. While a request is pending,
// canvas writes down each frame it draws, and InspectedEvent delivers the
// one render inspected, once render has delivered its own; every other
// frame pays one check, and allocates nothing for it.
func (a Assets) RequestInspection() {
	a.state.Get().inspector.requestPending.Store(true)
	a.frame.RequestInspection()
}

// ReadInspection answers event's inspection, with render's beside it. It
// answers it only while the jobs of event's publication run, so a handler
// of InspectedEvent calls it in a task it joins into the out dependency it
// returns; later, it answers an error.
func (a Assets) ReadInspection(event InspectedEvent) (Inspection, error) {
	inspection, ok := a.state.Get().inspector.read(event.Beat)
	if !ok {
		return Inspection{}, fmt.Errorf("canvas: reading the inspection of beat %d, which is read only while InspectedEvent's jobs run", event.Beat.Tick)
	}
	return inspection, nil
}
