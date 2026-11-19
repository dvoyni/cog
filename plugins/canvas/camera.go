package canvas

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// LayerRange is the layers from From up to, and not including, To; empty
// when To is not above From.
type LayerRange struct {
	From, To int32
}

// MakeLayer answers a camera's layers holding layer alone:
// canvas.MakeLayer(layerUI). math.MaxInt32, which no half-open range holds,
// panics.
func MakeLayer(layer int32) [4]LayerRange {
	if layer == math.MaxInt32 {
		panic("canvas: a camera's layers cannot hold layer math.MaxInt32, which no half-open range reaches")
	}
	return [4]LayerRange{{From: layer, To: layer + 1}}
}

// MakeLayers answers a camera's layers holding ranges, at most four, the
// rest empty: canvas.MakeLayers(canvas.LayerRange{From: layerGround, To:
// layerLabels + 1}, canvas.LayerRange{From: layerUI, To: layerUI + 1}). No
// range, or only empty ones, answers four empty ones, which mean every
// layer. More than four panics.
func MakeLayers(ranges ...LayerRange) [4]LayerRange {
	var layers [4]LayerRange
	if len(ranges) > len(layers) {
		panic(fmt.Sprintf("canvas: a camera's layers hold at most %d ranges, given %d", len(layers), len(ranges)))
	}
	copy(layers[:], ranges)
	return layers
}

// Camera is a view of the frame's entries into a target, a plain value set
// for one frame with DrawList.SetCamera and forgotten once drawn, as entries
// are: a game computes the camera it wants each frame, and keeps it to
// convert a click through. Its zero value sees every layer, into the whole
// of the main window, clearing nothing, at order 0, the world's origin at
// its viewport's centre, y down, one world unit to a point of a window or
// to a pixel of a texture.
type Camera struct {
	// Position is the world point the camera's Pivot shows. Rotation turns
	// the camera about it, in radians from +X towards +Y, as Rotation2x3
	// turns an entry, so the world shows turned the other way. Zoom scales
	// the view about it on top of the fit, zero meaning 1: at 2 the world
	// shows twice as large.
	Position m.Vec2
	Rotation float32
	Zoom     float32
	// Pivot is the point of the viewport that shows Position, and that
	// Rotation and Zoom turn and scale about, as a fraction of the viewport
	// from its centre, zero meaning the centre; under FitInscribe, of the
	// part between the bars. {-0.5, -0.5}, its top left, makes a UI camera
	// whose world coordinates are its viewport's points.
	Pivot m.Vec2
	// Layers are the layers the camera draws, each range half-open: an
	// empty range is unused, and four empty ones mean every layer.
	// MakeLayer answers the four holding one layer, and MakeLayers those
	// holding up to four ranges. A layer may be drawn by several cameras,
	// and an entry is drawn by every camera whose layers hold it.
	Layers [4]LayerRange
	// Window or Texture is the camera's target: a window platform opened,
	// or a texture the game made with render, of a colour format a pass
	// draws into, drawn at its mip level 0 and layer 0. Both zero mean the
	// main window, the first window opened of those still open. A camera
	// naming both, or a texture render does not hold, is skipped and
	// reported once, as ReportKindCameraTargetRefused.
	Window  platform.Window
	Texture render.Texture
	// Viewport is the part of the target the camera draws into, in
	// fractions of the target from its top left, its edges rounded to the
	// target's pixels; the zero rectangle means the whole target. Split
	// screen is two cameras, a viewport each.
	Viewport m.Rect
	// Clear clears the camera's viewport to ClearColor, a linear colour
	// encoded as the camera's output is, before it draws, FitInscribe's
	// bars included, and never the rest of its target, so the other half
	// of a split screen survives. A camera that clears draws even in a
	// frame with no entries.
	Clear      bool
	ClearColor m.Color
	// SampleCount is how many samples a pixel of the camera's entries takes, 1
	// or 4, zero meaning 1, DrawList.SetCamera panicking at any other. A
	// camera of 4 smooths every entry's hard edges at once: it draws into a
	// temporary multisampled texture of its viewport's size, cleared to
	// ClearColor when it clears and to transparent otherwise, resolves that
	// into a temporary texture of one sample, and draws it over its
	// viewport, replacing what the viewport holds when it clears and
	// blended over it otherwise, so the rest of its target survives, as a
	// resolve into the target, which writes all of it, would not. Its
	// entries blend against what it drew alone: in a camera of 4 that does
	// not clear, a material blending other than normally blends against
	// transparent, not against what the target holds.
	SampleCount uint32
	// Order is the order the camera draws in among every plugin's command
	// lists: canvas begins one list for each distinct order of the frame's
	// cameras, which render refuses and reports at an order another list
	// of the frame holds. Cameras of one order draw in one list, those into
	// a texture first, so a minimap is drawn before the window showing it,
	// then in the order they were set, by their draw lists' order and then
	// their calls'.
	Order render.Order
	// Size is the camera's logical size, the area of the world it is meant
	// to show, which Fit fits into its viewport. Zero on both axes is the
	// viewport's own size, one unit to a point of a window or a pixel of a
	// texture, so each half of a split screen shows half the window's
	// world; zero on one axis follows the viewport's aspect ratio, so
	// Size{Y: 1080} is 1080 units tall at any width.
	Size m.Vec2
	Fit  Fit
	// PixelPerfect, for pixel art, one art pixel a world unit, floors the
	// scale, fit and zoom together, to a whole number of the target's
	// pixels a unit, at least 1, so a 320×180 logical size draws 6 pixels a
	// unit in a 1080p window and 12 on a display of scale 2: FitExpand and
	// FitInscribe show more of the world or bars, and FitOverlap rounds it
	// up, so it still covers. FitStretch, whose pixels would not be square,
	// is drawn as FitInscribe and reported once, as
	// ReportKindPixelPerfectStretch. Each entry is moved whole so that its
	// first corner lies on the world-unit grid, rounded half up, by
	// canvas's vertex stage, and the view is placed on whole pixels, so the
	// world keeps its grid as the view scrolls a pixel at a time. A rotated
	// or scaled entry still draws at the target's resolution.
	PixelPerfect bool
}

// defaultCamera is the camera a frame that records entries and sets no
// camera is seen through: every layer, into the whole main window, cleared
// to opaque black, so it never shows a stale framebuffer, at order 0.
var defaultCamera = Camera{Clear: true, ClearColor: m.Color{A: 1}}

// ScreenToWorld answers the world point point shows, a point of the
// camera's target in a window's points or a texture's pixels, as platform's
// pointer events give a window's, through the camera's projection into the
// target as targets last saw it drawn, and whether the camera's entries
// draw there: in its viewport, but FitInscribe's bars. A target canvas has
// not drawn into, or a camera whose target is refused, answers false and the
// zero point. A click is routed by trying a frame's cameras from the
// highest Order down.
func (c *Camera) ScreenToWorld(point m.Vec2, targets Targets) (m.Vec2, bool) {
	view, pixelsPerPoint, ok := c.findView(targets)
	if !ok {
		return m.Vec2{}, false
	}
	pixel := point.MulS(pixelsPerPoint)
	// The camera's entries draw in its viewport, but its bars, their far
	// edges outside, so an edge two viewports share is one camera's.
	drawnRect := view.drawnRect.Float()
	inside := pixel.X >= drawnRect.X && pixel.X < drawnRect.X+drawnRect.Width && pixel.Y >= drawnRect.Y && pixel.Y < drawnRect.Y+drawnRect.Height
	return view.pixelToWorld.TransformPoint(pixel), inside
}

// WorldToScreen answers where point, a point of the world, shows in the
// camera's target, in a window's points or a texture's pixels, through the
// camera's projection into the target as targets last saw it drawn, and
// whether the camera's entries draw there, as ScreenToWorld does.
func (c *Camera) WorldToScreen(point m.Vec2, targets Targets) (m.Vec2, bool) {
	view, pixelsPerPoint, ok := c.findView(targets)
	if !ok {
		return m.Vec2{}, false
	}
	pixel := view.worldToPixel.TransformPoint(point)
	drawnRect := view.drawnRect.Float()
	inside := pixel.X >= drawnRect.X && pixel.X < drawnRect.X+drawnRect.Width && pixel.Y >= drawnRect.Y && pixel.Y < drawnRect.Y+drawnRect.Height
	return pixel.DivS(pixelsPerPoint), inside
}

// VisibleRect answers the world rectangle the camera's entries draw into,
// its bounding box when the camera turns, through the camera's projection
// into its target as targets last saw it drawn, and whether that target was
// drawn, as ScreenToWorld does: what a game spawns just outside of.
func (c *Camera) VisibleRect(targets Targets) (m.Rect, bool) {
	view, _, ok := c.findView(targets)
	if !ok {
		return m.Rect{}, false
	}
	return view.findVisibleRect(), true
}

// findView answers the camera's view into its target as targets last saw
// it drawn, that target's pixels to a point, and whether it has one: none
// for a target not drawn, a camera naming both a window and a texture, or
// one with no view there.
func (c *Camera) findView(targets Targets) (cameraView, float32, bool) {
	var size m.Vec2
	pixelsPerPoint := float32(1)
	var drawn bool
	switch {
	case c.Texture != (render.Texture{}) && c.Window != (platform.Window{}):
		return cameraView{}, 0, false
	case c.Texture != (render.Texture{}):
		size, drawn = targets.Texture(c.Texture)
	default:
		size, pixelsPerPoint, drawn = targets.Window(c.Window)
	}
	if !drawn {
		return cameraView{}, 0, false
	}
	// Targets holds a window's size in points, its pixels a whole number.
	pixels := size.MulS(pixelsPerPoint).Round().Int()
	view, ok := makeCameraView(c, pixels.X, pixels.Y, pixelsPerPoint)
	return view, pixelsPerPoint, ok
}

// findLayerRuns answers the runs of entries, the frame's in sort order,
// whose layers the camera draws, in sort order: each range's entries, one
// run since entries sort by layer first, ranges that overlap or meet merged
// so that each entry is in one run; or the whole of entries when every range
// is empty. The runs past the last are empty.
func (c *Camera) findLayerRuns(entries []sortedEntry) [4][]sortedEntry {
	ranges := c.Layers
	slices.SortFunc(ranges[:], func(a, b LayerRange) int { return cmp.Compare(a.From, b.From) })
	var runs [4][]sortedEntry
	// runStarts and runEnds are each run's bounds in entries, the first
	// runCount of them.
	var runStarts, runEnds [4]int
	runCount := 0
	everyRangeEmpty := true
	for _, layerRange := range ranges {
		if layerRange.To <= layerRange.From {
			continue
		}
		everyRangeEmpty = false
		// A layer's entries start at its key of the lowest order.
		runStart, _ := slices.BinarySearchFunc(entries, makeSortKey(layerRange.From, math.MinInt32), func(entry sortedEntry, key sortKey) int { return cmp.Compare(entry.key, key) })
		runEnd, _ := slices.BinarySearchFunc(entries, makeSortKey(layerRange.To, math.MinInt32), func(entry sortedEntry, key sortKey) int { return cmp.Compare(entry.key, key) })
		switch {
		case runStart == runEnd:
		case runCount > 0 && runStart <= runEnds[runCount-1]:
			runEnds[runCount-1] = max(runEnds[runCount-1], runEnd)
		default:
			runStarts[runCount] = runStart
			runEnds[runCount] = runEnd
			runCount++
		}
	}
	if everyRangeEmpty {
		runs[0] = entries
		return runs
	}
	for index := range runCount {
		runs[index] = entries[runStarts[index]:runEnds[index]]
	}
	return runs
}

// placeViewport answers the camera's viewport in a target width by height
// pixels: its fractions of the target, each edge rounded to a pixel and
// clamped to the target, so viewports sharing an edge neither overlap nor
// leave a gap; the whole target for the zero rectangle. It may be empty.
func (c *Camera) placeViewport(width, height int) m.Recti {
	if c.Viewport == (m.Rect{}) {
		return m.Recti{Width: width, Height: height}
	}
	placeEdge := func(fraction float32, size int) int {
		return min(max(int(math.Round(float64(fraction)*float64(size))), 0), size)
	}
	left := placeEdge(c.Viewport.X, width)
	top := placeEdge(c.Viewport.Y, height)
	right := placeEdge(c.Viewport.X+c.Viewport.Width, width)
	bottom := placeEdge(c.Viewport.Y+c.Viewport.Height, height)
	return m.Recti{X: left, Y: top, Width: max(right-left, 0), Height: max(bottom-top, 0)}
}

// compareCameras orders cameras as they draw: by order, and at equal orders
// those into a texture before those into a window; a stable sort keeps the
// order they were set in among the rest.
func compareCameras(a, b Camera) int {
	drawsIntoWindow := func(camera Camera) int {
		if camera.Texture != (render.Texture{}) {
			return 0
		}
		return 1
	}
	return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(drawsIntoWindow(a), drawsIntoWindow(b)))
}

// Fit is how a camera fits its logical size into a viewport of another
// shape.
type Fit uint8

const (
	// FitExpand, the zero Fit, scales the logical size to fit inside the
	// viewport, the viewport's longer axis showing more of the world.
	FitExpand Fit = iota
	// FitInscribe scales it as FitExpand does and draws the logical size
	// alone, centred, the rest of the viewport bars, cleared to ClearColor
	// by a camera that clears and left as the target holds them by one that
	// does not.
	FitInscribe
	// FitOverlap scales it to cover the viewport, its edges cut off.
	FitOverlap
	// FitStretch scales each axis on its own to fill the viewport, so a
	// world unit is no longer square, and a hairline is one pixel wide
	// along the axis of the smaller scale and wider along the other.
	FitStretch
)
