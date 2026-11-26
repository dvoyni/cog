package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// multisampledCameraSampleCount is the samples of a multisampled camera,
// the one count above 1 a Camera's SampleCount takes.
const multisampledCameraSampleCount = 4

// cameraPlan is how a camera draws into its target, decided once from the
// camera and its measured target, before any entry is found: the target;
// the camera's view into it and its entries' pass parameters; the colour
// target of its entries' pass, with its load, the target's or, for a
// multisampled camera, a texture of multisampledDescriptor, the textures named once
// acquired; colorTargetOrigin, where that colour target's top left lies in
// the target; how the camera clears; the quad drawn over its viewport; and
// its composite of what a multisampled camera resolved, into
// compositeTarget, with its load.
type cameraPlan struct {
	target                 drawnTarget
	view                   cameraView
	block                  cameraBlock
	entriesTarget          render.ColorTarget
	multisampledDescriptor render.TextureDescriptor
	colorTargetOrigin      m.Vec2i
	clear                  cameraClear
	viewportQuad           viewportQuad
	composite              cameraComposite
	compositeTarget        render.ColorTarget
}

// cameraClear is how a camera clears its viewport.
type cameraClear uint8

const (
	// clearNone clears nothing: the camera does not clear.
	clearNone cameraClear = iota
	// clearByLoad clears by the load of its entries' pass: its viewport
	// covers the target, or it draws into a multisampled texture of its
	// viewport's size.
	clearByLoad
	// clearByQuad draws its ClearColor over its viewport, the rest of the
	// target kept.
	clearByQuad
)

// viewportQuad is the quad a camera draws over its viewport, its instance
// appended beside its batches'.
type viewportQuad uint8

const (
	// quadNone draws no quad.
	quadNone viewportQuad = iota
	// quadClear draws the camera's ClearColor, its clear by quad.
	quadClear
	// quadResolved draws what a multisampled camera resolved, whole and
	// untinted, its composite.
	quadResolved
)

// cameraComposite is how a multisampled camera draws what it resolved over
// its viewport.
type cameraComposite uint8

const (
	// compositeNone draws no composite: the camera is of one sample.
	compositeNone cameraComposite = iota
	// compositeReplace replaces what the viewport holds, the camera
	// clearing.
	compositeReplace
	// compositeBlend blends over what the viewport holds, the camera not
	// clearing.
	compositeBlend
)

// makeCameraPlan answers how camera draws into target, and false when it has
// no view, as of an empty viewport. Its output is encoded wherever the
// target's format encodes nothing, as a window's BGRA8Unorm framebuffer
// does not. A camera of one sample draws into the target, cleared by the
// pass's load when the camera clears a viewport covering the target, and
// with its ClearColor drawn over the viewport when it clears a part of it.
// A multisampled camera draws into a texture of its viewport's size in the
// target's format, the viewport at the texture's origin, cleared by the
// load to its ClearColor or, not clearing, transparent; it then draws what
// it resolved over its viewport, replacing what it holds when it clears,
// the target's load clearing it whole when the viewport covers it, its
// colour unused, and blended over it otherwise.
func makeCameraPlan(camera *Camera, target drawnTarget) (cameraPlan, bool) {
	view, ok := makeCameraView(camera, target.width, target.height, target.pixelsPerPoint)
	if !ok {
		return cameraPlan{}, false
	}
	plan := cameraPlan{target: target, view: view}
	encode := target.format != render.TextureFormatRGBA8UnormSrgb && target.format != render.TextureFormatBGRA8UnormSrgb
	multisampled := camera.SampleCount == multisampledCameraSampleCount
	if multisampled {
		plan.colorTargetOrigin = view.viewport.Min()
	}
	plan.block = makeCameraBlock(&view, encode, plan.colorTargetOrigin)
	keptTarget := render.ColorTarget{Window: target.window, Texture: target.texture}
	var loadedClearColor render.Color
	if camera.Clear {
		loadedClearColor = makeLoadedClearColor(camera.ClearColor, encode)
	}
	coversTarget := view.viewport == m.Recti{Width: target.width, Height: target.height}
	if !multisampled {
		plan.entriesTarget = keptTarget
		switch {
		case !camera.Clear:
		case coversTarget:
			plan.clear = clearByLoad
			plan.entriesTarget.Clear = true
			plan.entriesTarget.ClearColor = loadedClearColor
		default:
			plan.clear = clearByQuad
			plan.viewportQuad = quadClear
		}
		return plan, true
	}
	plan.multisampledDescriptor = render.TextureDescriptor{
		Label:       "canvas multisampled camera",
		Width:       uint32(view.viewport.Width),
		Height:      uint32(view.viewport.Height),
		SampleCount: multisampledCameraSampleCount,
		Format:      target.format,
	}
	plan.entriesTarget = render.ColorTarget{Clear: true, Discard: true}
	plan.viewportQuad = quadResolved
	plan.composite = compositeBlend
	plan.compositeTarget = keptTarget
	if camera.Clear {
		plan.clear = clearByLoad
		plan.entriesTarget.ClearColor = loadedClearColor
		plan.composite = compositeReplace
		if coversTarget {
			plan.compositeTarget.Clear = true
			plan.compositeTarget.ClearColor = loadedClearColor
		}
	}
	return plan, true
}

// makeLoadedClearColor answers color, linear, as a pass's load clears its
// target to it: premultiplied, as canvas blends, and its colour encoded as
// sRGB when encode is set, for a target whose format encodes nothing, as
// canvas_output encodes what the camera draws, so a clear by the load and
// one drawn over a viewport show alike.
func makeLoadedClearColor(color m.Color, encode bool) render.Color {
	if encode {
		red, green, blue, alpha := color.Srgb()
		color = m.Color{R: red, G: green, B: blue, A: alpha}
	}
	return render.Color{R: float64(color.R * color.A), G: float64(color.G * color.A), B: float64(color.B * color.A), A: float64(color.A)}
}
