package canvas

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// windowTargets is a Targets holding one window drawn, answered for any
// window asked of it, the zero one included, and no texture.
type windowTargets struct {
	// size is the window's size in points, and scale its pixels to a point.
	size  m.Vec2
	scale float32
}

func (t windowTargets) Window(platform.Window) (m.Vec2, float32, bool) {
	return t.size, t.scale, true
}

func (windowTargets) Texture(render.Texture) (m.Vec2, bool) { return m.Vec2{}, false }

func TestEachFitScalesALogicalSizeIntoAWideAndATallViewportAndZoomAndRotationApplyOnTop(t *testing.T) {
	// A logical size 100 units square, into a window of 400×200 points, or
	// 200×400, at one pixel a point, centred on the world's origin unless
	// a case moves it.
	wide := m.Vec2{X: 400, Y: 200}
	tall := m.Vec2{X: 200, Y: 400}
	square := m.Vec2{X: 100, Y: 100}
	for _, view := range []struct {
		name    string
		window  m.Vec2
		camera  Camera
		scale   m.Vec2
		visible m.Rect
	}{
		{"expanded into a wide viewport", wide, Camera{Size: square}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -100, Y: -50, Width: 200, Height: 100}},
		{"inscribed into a wide viewport", wide, Camera{Size: square, Fit: FitInscribe}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -50, Y: -50, Width: 100, Height: 100}},
		{"overlapping a wide viewport", wide, Camera{Size: square, Fit: FitOverlap}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -50, Y: -25, Width: 100, Height: 50}},
		{"stretched into a wide viewport", wide, Camera{Size: square, Fit: FitStretch}, m.Vec2{X: 4, Y: 2}, m.Rect{X: -50, Y: -50, Width: 100, Height: 100}},
		{"expanded into a tall viewport", tall, Camera{Size: square}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -50, Y: -100, Width: 100, Height: 200}},
		{"inscribed into a tall viewport", tall, Camera{Size: square, Fit: FitInscribe}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -50, Y: -50, Width: 100, Height: 100}},
		{"overlapping a tall viewport", tall, Camera{Size: square, Fit: FitOverlap}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -25, Y: -50, Width: 50, Height: 100}},
		{"stretched into a tall viewport", tall, Camera{Size: square, Fit: FitStretch}, m.Vec2{X: 2, Y: 4}, m.Rect{X: -50, Y: -50, Width: 100, Height: 100}},
		{"expanded into a wide viewport at zoom 2", wide, Camera{Size: square, Zoom: 2}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -50, Y: -25, Width: 100, Height: 50}},
		{"inscribed into a tall viewport at zoom 2, its bars kept", tall, Camera{Size: square, Fit: FitInscribe, Zoom: 2}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -25, Y: -25, Width: 50, Height: 50}},
		{"stretched into a wide viewport at zoom 0.5", wide, Camera{Size: square, Fit: FitStretch, Zoom: 0.5}, m.Vec2{X: 2, Y: 1}, m.Rect{X: -100, Y: -100, Width: 200, Height: 200}},
		{"expanded into a wide viewport, turned a quarter, from (10, 20)", wide, Camera{Size: square, Rotation: math.Pi / 2, Position: m.Vec2{X: 10, Y: 20}}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -40, Y: -80, Width: 100, Height: 200}},
		{"overlapping a tall viewport, turned an eighth", tall, Camera{Size: square, Fit: FitOverlap, Rotation: math.Pi / 4}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -53.033009, Y: -53.033009, Width: 106.066017, Height: 106.066017}},
		{"of a zero height, following the viewport's shape", wide, Camera{Size: m.Vec2{X: 100}}, m.Vec2{X: 4, Y: 4}, m.Rect{X: -50, Y: -25, Width: 100, Height: 50}},
		{"of a zero width, following the viewport's shape", wide, Camera{Size: m.Vec2{Y: 100}}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -100, Y: -50, Width: 200, Height: 100}},
		{"of a zero width, following the viewport's shape, overlapping it", wide, Camera{Size: m.Vec2{Y: 100}, Fit: FitOverlap}, m.Vec2{X: 2, Y: 2}, m.Rect{X: -100, Y: -50, Width: 200, Height: 100}},
		{"of no logical size, one unit to a point", wide, Camera{}, m.Vec2{X: 1, Y: 1}, m.Rect{X: -200, Y: -100, Width: 400, Height: 200}},
		{"of no logical size with its pivot at its top left", wide, Camera{Pivot: m.Vec2{X: -0.5, Y: -0.5}}, m.Vec2{X: 1, Y: 1}, m.Rect{Width: 400, Height: 200}},
	} {
		targets := windowTargets{size: view.window, scale: 1}
		drawnView, ok := makeCameraView(&view.camera, int(view.window.X), int(view.window.Y), 1)
		if !ok {
			t.Errorf("a camera %s has no view", view.name)
			continue
		}
		if !nearVec2(drawnView.scale, view.scale) {
			t.Errorf("a camera %s draws %v pixels a unit, want %v", view.name, drawnView.scale, view.scale)
		}
		visible, drawn := view.camera.VisibleRect(targets)
		if !drawn || !nearRect(visible, view.visible) {
			t.Errorf("a camera %s sees %+v, drawn %v, want %+v, drawn", view.name, visible, drawn, view.visible)
		}
	}
}

func TestAWindowsCameraOfNoLogicalSizeDrawsAUnitToAPointWhateverItsScale(t *testing.T) {
	targets := windowTargets{size: m.Vec2{X: 200, Y: 100}, scale: 2}
	camera := Camera{}

	drawnView, _ := makeCameraView(&camera, 400, 200, 2)
	visible, _ := camera.VisibleRect(targets)

	if want := (m.Vec2{X: 2, Y: 2}); drawnView.scale != want {
		t.Errorf("a camera of no logical size into a window at scale 2 draws %v pixels a unit, want %v", drawnView.scale, want)
	}
	if want := (m.Rect{X: -100, Y: -50, Width: 200, Height: 100}); !nearRect(visible, want) {
		t.Errorf("it sees %+v, want the window's points, %+v", visible, want)
	}
}

func TestAnInscribedCamerasBarsAreTheViewportOutsideItsLogicalSizeRoundedToPixels(t *testing.T) {
	// 100 units square into 401×200 pixels, two a unit: 200 pixels across,
	// 100.5 of bar each side, rounded alike, so the bars match.
	camera := Camera{Size: m.Vec2{X: 100, Y: 100}, Fit: FitInscribe, Viewport: m.Rect{Width: 1, Height: 1}}

	drawnView, _ := makeCameraView(&camera, 401, 200, 1)

	if want := (m.Recti{X: 101, Width: 199, Height: 200}); drawnView.drawnRect != want {
		t.Errorf("the inscribed camera draws its entries into %+v, want %+v, between bars", drawnView.drawnRect, want)
	}
	if want := (m.Recti{Width: 401, Height: 200}); drawnView.viewport != want {
		t.Errorf("its viewport is %+v, want %+v, bars included", drawnView.viewport, want)
	}
}

func TestAConversionMapsAPointOfTheWindowToTheWorldAndBackAndAnswersWhetherTheCameraDrawsThere(t *testing.T) {
	// A UI camera, its world the window's points, and an inscribed one 100
	// units square turned a quarter, in a window of 400×200 points at scale
	// 2.
	targets := windowTargets{size: m.Vec2{X: 400, Y: 200}, scale: 2}
	ui := Camera{Pivot: m.Vec2{X: -0.5, Y: -0.5}}
	turned := Camera{Size: m.Vec2{X: 100, Y: 100}, Fit: FitInscribe, Rotation: math.Pi / 2, Position: m.Vec2{X: 1000}}
	for _, conversion := range []struct {
		name   string
		camera Camera
		screen m.Vec2
		world  m.Vec2
		inside bool
	}{
		{"the UI camera's top left", ui, m.Vec2{}, m.Vec2{}, true},
		{"a point of the UI camera's", ui, m.Vec2{X: 30.5, Y: 40}, m.Vec2{X: 30.5, Y: 40}, true},
		{"a point past the UI camera's bottom right", ui, m.Vec2{X: 400, Y: 10}, m.Vec2{X: 400, Y: 10}, false},
		{"the turned camera's centre", turned, m.Vec2{X: 200, Y: 100}, m.Vec2{X: 1000}, true},
		// Two points a unit, the camera turned a quarter from +X towards
		// +Y: the screen's right shows the world's +Y, and its down the
		// world's -X.
		{"a point right of the turned camera's centre", turned, m.Vec2{X: 220, Y: 100}, m.Vec2{X: 1000, Y: 10}, true},
		{"a point below the turned camera's centre", turned, m.Vec2{X: 200, Y: 130}, m.Vec2{X: 985}, true},
		{"a point on the turned camera's left bar", turned, m.Vec2{X: 80, Y: 100}, m.Vec2{X: 1000, Y: -60}, false},
	} {
		world, inside := conversion.camera.ScreenToWorld(conversion.screen, targets)
		if !nearVec2(world, conversion.world) || inside != conversion.inside {
			t.Errorf("%s, %v, converts to the world's %v, inside %v, want %v, inside %v", conversion.name, conversion.screen, world, inside, conversion.world, conversion.inside)
		}
		screen, inside := conversion.camera.WorldToScreen(conversion.world, targets)
		if !nearVec2(screen, conversion.screen) || inside != conversion.inside {
			t.Errorf("%s, the world's %v, converts to the window's %v, inside %v, want %v, inside %v", conversion.name, conversion.world, screen, inside, conversion.screen, conversion.inside)
		}
	}
}

func TestAConversionThroughATargetNotDrawnAnswersFalse(t *testing.T) {
	camera := Camera{}

	_, toWorld := camera.ScreenToWorld(m.Vec2{X: 1, Y: 1}, noTargets{})
	_, toScreen := camera.WorldToScreen(m.Vec2{X: 1, Y: 1}, noTargets{})
	_, visible := camera.VisibleRect(noTargets{})

	if toWorld || toScreen || visible {
		t.Errorf("conversions through a window not drawn answer %v, %v and %v, want false", toWorld, toScreen, visible)
	}
}

// noTargets is a Targets of nothing drawn.
type noTargets struct{}

func (noTargets) Window(platform.Window) (m.Vec2, float32, bool) { return m.Vec2{}, 0, false }

func (noTargets) Texture(render.Texture) (m.Vec2, bool) { return m.Vec2{}, false }

// nearVec2 answers whether got is want, but for a float32's rounding.
func nearVec2(got, want m.Vec2) bool {
	return math.Abs(float64(got.X-want.X)) < 1e-3 && math.Abs(float64(got.Y-want.Y)) < 1e-3
}

// nearRect answers whether got is want, but for a float32's rounding.
func nearRect(got, want m.Rect) bool {
	return nearVec2(m.Vec2{X: got.X, Y: got.Y}, m.Vec2{X: want.X, Y: want.Y}) && nearVec2(m.Vec2{X: got.Width, Y: got.Height}, m.Vec2{X: want.Width, Y: want.Height})
}
