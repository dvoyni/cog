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
// frame's recording has ended, and canvas packs it as it is first drawn. An
// image packed already, or preloaded and not yet drawn, is not loaded
// again, nor is the zero Image, the white texel, nor an image of a render
// texture, ImageFromTexture's, which loads nothing.
func (a Assets) Preload(image Image) {
	if image == (Image{}) {
		return
	}
	key := imageNames.findKey(image)
	if key.ofTexture {
		return
	}
	a.state.Get().residency.requestPreload(a.inventory, sourceKey{path: key.path, colorSpace: key.options.ColorSpace})
}

// Unload unloads the images of path, or, when path ends in a slash, of
// every path in the folder it names, so Unload("level1/") unloads a
// level's art and not level10's: their regions of the sprite atlases are
// freed and their own textures released as canvas next draws, and a later
// draw loads them again. Their sizes ImageSize read are forgotten, and the
// reports canvas made of them once may be made again.
func (a Assets) Unload(path string) {
	state := a.state.Get()
	state.residency.requestUnload(a.inventory, path)
	state.imageSizes.forget(state.logger, path)
	for _, unloaded := range imageNames.findPaths(path) {
		state.logger.ForgetLogOnce(imageOptionsIgnoredLogKey + unloaded)
		state.logger.ForgetLogOnce(frameOutOfBoundsLogKey + unloaded)
	}
}

// ImageSize answers image's size in texels before it is drawn, its
// SourceRect's, or its sheet's frames', read from its file's header alone,
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
		size, read = state.imageSizes.find(a.files.Get(), state.logger, key.path)
	}
	if !read {
		return m.Vec2{}, false
	}
	frameRect, withinFile := key.options.findFrameRect(int(size.X), int(size.Y), 0)
	return frameRect.Size().Float(), withinFile
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
