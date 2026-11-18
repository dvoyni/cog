package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/platform"
	"prototype.invalid/canvasframe/render"
)

// Camera is how a frame's entries are seen: a plain value, set on a list each
// frame with SetCamera and forgotten once the frame is drawn.
type Camera struct {
	// Position is the world point placed at Pivot; Rotation turns the view
	// and Zoom scales it about Pivot, zero meaning 1.
	Position m.Vec2
	Rotation float32
	Zoom     float32
	// Pivot is a point of the viewport as a fraction from its centre, zero
	// meaning the centre. A UI camera takes {-0.5, -0.5}, its top left, so
	// world coordinates are UI coordinates.
	Pivot m.Vec2
	// Layers are the layer ranges drawn, each half-open [From, To). An empty
	// range is unused, and four empty ones mean every layer.
	Layers [4]LayerRange
	// Window or Texture is the target, both zero meaning the main window.
	Window  platform.Window
	Texture render.Texture
	// Viewport is the rectangle of the target drawn into, in fractions of
	// it, zero meaning all of it.
	Viewport m.Rect
	// Clear clears the viewport, never the rest of its target, to ClearColor.
	Clear      bool
	ClearColor m.Color
	// Samples is 1 or 4, zero meaning 1, resolved into the target.
	Samples uint32
	// Order is the order cameras draw in; at equal Order texture targets draw
	// before windows.
	Order int32
	// Size is the logical size, the world the camera is meant to show, fitted
	// into the viewport by Fit. Zero on both axes is the viewport's own size
	// in points, one unit to a point; zero on one axis follows the viewport's
	// aspect ratio.
	Size m.Vec2
	Fit  Fit
	// PixelPerfect floors the scale to a whole number of framebuffer pixels
	// per world unit and snaps entries to the world-unit grid.
	PixelPerfect bool
}

// LayerRange is the layers From to To, To excluded: layer 0 alone is {0, 1}.
type LayerRange struct{ From, To int32 }

// Layers answers a camera's layer ranges holding the one range from to to,
// to excluded: Layers: canvas.Layers(layerUI, layerUI+1). Several ranges
// are a literal.
func Layers(from, to int32) [4]LayerRange { return [4]LayerRange{{From: from, To: to}} }

// Fit is how a camera's logical size fills a viewport of another shape.
type Fit uint8

const (
	// FitExpand takes the smaller scale, the longer axis showing more of the
	// world.
	FitExpand Fit = iota
	// FitInscribe takes the smaller scale and fills the rest with bars of
	// ClearColor.
	FitInscribe
	// FitOverlap takes the larger scale, cutting the edges off.
	FitOverlap
	// FitStretch scales each axis on its own.
	FitStretch
)

// SetCamera sets camera for this frame. A frame that sets none is seen
// through the default camera: the main window, every layer, cleared to opaque
// black.
func (l DrawList) SetCamera(camera Camera) { l.recordCamera(camera) }

// Targets is canvas's resource of each target's size and scale as it was
// last drawn, written by canvas's drawing once a frame: its reads alone.
type Targets interface {
	// Window answers window's size in points and its scale, and whether it
	// has been drawn.
	Window(window platform.Window) (size m.Vec2, scale float32, drawn bool)
	// Texture answers texture's size in pixels, and whether it has been
	// drawn.
	Texture(texture render.Texture) (size m.Vec2, drawn bool)
}

// ScreenToWorld answers the world point under point, in the target's points,
// and whether it lies in the camera's viewport of a target drawn.
func (c Camera) ScreenToWorld(point m.Vec2, targets Targets) (m.Vec2, bool) {
	view, ok := c.view(targets)
	if !ok {
		return m.Vec2{}, false
	}
	return view.toWorld(point)
}

// WorldToScreen answers where point lies on the target, in its points, and
// whether that is in the camera's viewport of a target drawn.
func (c Camera) WorldToScreen(point m.Vec2, targets Targets) (m.Vec2, bool) {
	view, ok := c.view(targets)
	if !ok {
		return m.Vec2{}, false
	}
	return view.toScreen(point)
}

// VisibleRect answers the world rectangle the camera sees, its bounding box
// when it rotates, and whether its target has been drawn.
func (c Camera) VisibleRect(targets Targets) (m.Rect, bool) {
	view, ok := c.view(targets)
	return view.visible, ok
}
