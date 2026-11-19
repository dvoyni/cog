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
// between the world and the target's pixels.
type cameraView struct {
	viewport, drawnRect        m.Recti
	scale                      m.Vec2
	worldToPixel, pixelToWorld m.Mat2x3
}

// makeCameraView answers camera's view into a target width by height
// pixels, pixelsPerPoint of them to a point of a window, 1 for a texture,
// and whether it has one: none for an empty viewport, or a projection that
// collapses, as a logical size of an infinite axis does.
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
	uniformScale := min(across, down)
	if camera.Fit == FitOverlap {
		uniformScale = max(across, down)
	}
	fitScale := m.Vec2{X: uniformScale, Y: uniformScale}
	if camera.Fit == FitStretch {
		fitScale = m.Vec2{X: across, Y: down}
	}
	view.drawnRect = view.viewport
	if camera.Fit == FitInscribe {
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
	view.scale = fitScale.MulS(zoom)
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
	pixelToWorld, invertible := view.worldToPixel.Inverse()
	if !invertible {
		return cameraView{}, false
	}
	view.pixelToWorld = pixelToWorld
	return view, true
}

// findVisibleRect answers the world rectangle the camera's entries draw
// into: drawnRect's corners in the world, their bounding box when the
// camera turns.
func (v *cameraView) findVisibleRect() m.Rect {
	drawnRect := v.drawnRect.Float()
	topLeft := drawnRect.Min()
	bottomRight := drawnRect.Max()
	low := v.pixelToWorld.TransformPoint(topLeft)
	high := low
	for _, corner := range [3]m.Vec2{{X: bottomRight.X, Y: topLeft.Y}, {X: topLeft.X, Y: bottomRight.Y}, bottomRight} {
		world := v.pixelToWorld.TransformPoint(corner)
		low = m.Vec2{X: min(low.X, world.X), Y: min(low.Y, world.Y)}
		high = m.Vec2{X: max(high.X, world.X), Y: max(high.Y, world.Y)}
	}
	return m.Rect{X: low.X, Y: low.Y, Width: high.X - low.X, Height: high.Y - low.Y}
}
