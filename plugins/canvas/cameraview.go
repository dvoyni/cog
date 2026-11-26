package canvas

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// cameraView is a camera's projection into a target, which its drawing and
// its conversions both build, so a click converts through the projection
// drawn: its viewport in the target's pixels; drawnRect, the part of the
// viewport its entries draw into, all of it but under FitInscribe, whose
// bars are the rest; its scale, the target's pixels to a world unit along
// each of the view's axes, fit and zoom together; and its transforms
// between the world and the target's pixels; and snap, the grid in world
// units its entries snap to, one unit for a pixel-perfect camera and zero
// for any other.
type cameraView struct {
	viewport, drawnRect        m.Recti
	scale                      m.Vec2
	worldToPixel, pixelToWorld m.Mat2x3
	snap                       float32
}

// makeCameraView answers camera's view into a target width by height
// pixels, pixelsPerPoint of them to a point of a window, 1 for a texture,
// and whether it has one: none for an empty viewport, or a projection that
// collapses, as a logical size of an infinite axis does. A pixel-perfect
// camera's FitStretch is drawn as FitInscribe, its scale a whole number of
// pixels a unit, and its placement on whole pixels.
func makeCameraView(camera *Camera, width, height int, pixelsPerPoint float32) (cameraView, bool) {
	view := cameraView{viewport: camera.placeViewport(width, height)}
	if view.viewport.Width == 0 || view.viewport.Height == 0 {
		return cameraView{}, false
	}
	viewportWidth := float32(view.viewport.Width)
	viewportHeight := float32(view.viewport.Height)
	logicalSize := camera.Size
	switch {
	case logicalSize.X == 0 && logicalSize.Y == 0:
		logicalSize = m.Vec2{X: viewportWidth / pixelsPerPoint, Y: viewportHeight / pixelsPerPoint}
	case logicalSize.X == 0:
		logicalSize.X = logicalSize.Y * viewportWidth / viewportHeight
	case logicalSize.Y == 0:
		logicalSize.Y = logicalSize.X * viewportHeight / viewportWidth
	}
	// fitScale is the pixels to a unit that fit the logical size into the
	// viewport, along each axis.
	across := viewportWidth / logicalSize.X
	down := viewportHeight / logicalSize.Y
	fit := camera.Fit
	if camera.PixelPerfect && fit == FitStretch {
		fit = FitInscribe
	}
	uniformScale := min(across, down)
	if fit == FitOverlap {
		uniformScale = max(across, down)
	}
	if camera.PixelPerfect {
		uniformScale = makeWholeScale(uniformScale, fit == FitOverlap)
	}
	fitScale := m.Vec2{X: uniformScale, Y: uniformScale}
	if fit == FitStretch {
		fitScale = m.Vec2{X: across, Y: down}
	}
	view.drawnRect = view.viewport
	if fit == FitInscribe {
		// The logical size at the fit's scale, centred, each bar rounded
		// to pixels alike.
		barWidth := max(int(math.Round(float64(viewportWidth-logicalSize.X*fitScale.X)/2)), 0)
		barHeight := max(int(math.Round(float64(viewportHeight-logicalSize.Y*fitScale.Y)/2)), 0)
		view.drawnRect = m.Recti{
			X:      view.viewport.X + barWidth,
			Y:      view.viewport.Y + barHeight,
			Width:  max(view.viewport.Width-2*barWidth, 0),
			Height: max(view.viewport.Height-2*barHeight, 0),
		}
	}
	zoom := camera.Zoom
	if zoom == 0 {
		zoom = 1
	}
	if camera.PixelPerfect {
		// The scale, fit and zoom together, is whole; the bars stay those
		// of the fit alone, as zoom leaves them.
		whole := makeWholeScale(uniformScale*zoom, fit == FitOverlap)
		view.scale = m.Vec2{X: whole, Y: whole}
		view.snap = 1
	} else {
		view.scale = fitScale.MulS(zoom)
	}
	pivot := m.Vec2{
		X: float32(view.drawnRect.X) + (0.5+camera.Pivot.X)*float32(view.drawnRect.Width),
		Y: float32(view.drawnRect.Y) + (0.5+camera.Pivot.Y)*float32(view.drawnRect.Height),
	}
	// The world turns against the camera, about Position, is scaled along
	// the view's axes, and Position lands on the pivot.
	view.worldToPixel = m.Translation2x3(pivot.X, pivot.Y).
		Mul(m.Scaling2x3(view.scale.X, view.scale.Y)).
		Mul(m.Rotation2x3(-camera.Rotation)).
		Mul(m.Translation2x3(-camera.Position.X, -camera.Position.Y))
	if camera.PixelPerfect {
		// The world's origin lands on a whole pixel, rounded half up, so
		// the world keeps its grid on the target's pixels as the view
		// scrolls a pixel at a time.
		origin := m.Vec2{X: view.worldToPixel[4], Y: view.worldToPixel[5]}.AddS(0.5).Floor()
		view.worldToPixel[4] = origin.X
		view.worldToPixel[5] = origin.Y
	}
	pixelToWorld, invertible := view.worldToPixel.Inverse()
	if !invertible {
		return cameraView{}, false
	}
	view.pixelToWorld = pixelToWorld
	return view, true
}

// makeWholeScale answers scale, pixels a unit, as a pixel-perfect camera
// draws it: a whole number, at least 1, rounded up when up is set, as
// FitOverlap's is so that it still covers the viewport, and down otherwise.
// A scale a float32's rounding short of a whole number is that number.
func makeWholeScale(scale float32, up bool) float32 {
	if up {
		return max(float32(math.Ceil(float64(scale)-1e-3)), 1)
	}
	return max(float32(math.Floor(float64(scale)+1e-3)), 1)
}

// findVisibleRect answers the world rectangle the camera's entries draw
// into: the bounding box of drawnRect's corners in the world, drawnRect's
// own when the camera does not turn.
func (v *cameraView) findVisibleRect() m.Rect {
	return v.pixelToWorld.TransformRect(v.drawnRect.Float())
}

// appendVisibleEntries appends to visible the entries of layerRun, each its
// drawn entry of drawn at its index, holding its bounds and its batch key's
// clip, in order, that the view does not cull, and answers visible: those
// whose bounds meet its visible rectangle, or the part of it their clip
// holds where they have one, grown by half its snap, the most a snapped
// entry moves.
func (v *cameraView) appendVisibleEntries(visible, layerRun []sortedEntry, drawn []drawnEntry) []sortedEntry {
	visibleRect := v.findVisibleRect()
	margin := m.Vec2{X: -v.snap / 2, Y: -v.snap / 2}
	grownVisibleRect := visibleRect.Inset(margin)
	for _, entry := range layerRun {
		cullRect := grownVisibleRect
		entryDrawn := &drawn[entry.entryIndex]
		if clip := entryDrawn.key.clip; clip != (m.Rect{}) {
			clipVisibleRect, clipVisible := visibleRect.Intersection(clip)
			if !clipVisible {
				continue
			}
			cullRect = clipVisibleRect.Inset(margin)
		}
		if entryDrawn.bounds.Intersects(cullRect) {
			visible = append(visible, entry)
		}
	}
	return visible
}

// placeClip answers clip, a rectangle of the world, as the scissor of the
// entries it clips: the bounding box of its corners in the target's pixels
// within drawnRect, each edge rounded half up to a pixel's, so a pixel is
// drawn where its centre lies inside, drawnRect itself for no clip; and
// whether it holds a pixel.
func (v *cameraView) placeClip(clip m.Rect) (m.Recti, bool) {
	if clip == (m.Rect{}) {
		return v.drawnRect, true
	}
	// Rectangles that miss intersect in the zero one, which holds no pixel.
	pixels, _ := v.worldToPixel.TransformRect(clip).Intersection(v.drawnRect.Float())
	low := pixels.Min().AddS(0.5).Floor()
	high := pixels.Max().AddS(0.5).Floor()
	scissor := m.Recti{X: int(low.X), Y: int(low.Y), Width: int(high.X - low.X), Height: int(high.Y - low.Y)}
	if scissor.Width <= 0 || scissor.Height <= 0 {
		return m.Recti{}, false
	}
	return scissor, true
}
