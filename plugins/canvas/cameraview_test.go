package canvas

import (
	"math"
	"slices"
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

func TestAPixelPerfectCamerasScaleIsAWholeNumberOfPixelsAUnitTheBarsOfItsFitAlone(t *testing.T) {
	// A logical size of 320×180 art pixels, centred on the world's origin.
	art := m.Vec2{X: 320, Y: 180}
	for _, view := range []struct {
		name          string
		width, height int
		scale         float32
		camera        Camera
		// drawnRect is where the camera's entries draw, the zero rectangle
		// meaning its whole viewport.
		drawnRect m.Recti
		scaled    float32
	}{
		{"into a 1280×720 window at scale 2", 2560, 1440, 2, Camera{Size: art}, m.Recti{}, 8},
		{"into a 1080p window", 1920, 1080, 1, Camera{Size: art}, m.Recti{}, 6},
		{"expanded into 1000×720, floored", 1000, 720, 1, Camera{Size: art}, m.Recti{}, 3},
		{"inscribed into 1000×720, floored, the rest bars", 1000, 720, 1, Camera{Size: art, Fit: FitInscribe}, m.Recti{X: 20, Y: 90, Width: 960, Height: 540}, 3},
		{"inscribed into 1000×720 at zoom 1.5, floored, its bars kept", 1000, 720, 1, Camera{Size: art, Fit: FitInscribe, Zoom: 1.5}, m.Recti{X: 20, Y: 90, Width: 960, Height: 540}, 4},
		{"stretched into 1000×720, drawn as inscribed", 1000, 720, 1, Camera{Size: art, Fit: FitStretch}, m.Recti{X: 20, Y: 90, Width: 960, Height: 540}, 3},
		{"overlapping 1000×700, rounded up", 1000, 700, 1, Camera{Size: art, Fit: FitOverlap}, m.Recti{}, 4},
		{"overlapping a 1080p window at zoom 0.3, rounded up", 1920, 1080, 1, Camera{Size: art, Fit: FitOverlap, Zoom: 0.3}, m.Recti{}, 2},
		{"into a 1280×720 window at scale 2 at zoom 0.5", 2560, 1440, 2, Camera{Size: art, Zoom: 0.5}, m.Recti{}, 4},
		{"into a viewport smaller than its logical size, at least 1", 200, 100, 1, Camera{Size: art}, m.Recti{}, 1},
		{"of a zero width following the viewport's shape", 2560, 1440, 2, Camera{Size: m.Vec2{Y: 180}}, m.Recti{}, 8},
	} {
		view.camera.PixelPerfect = true
		drawnView, ok := makeCameraView(&view.camera, view.width, view.height, view.scale)
		if !ok {
			t.Errorf("a pixel-perfect camera %s has no view", view.name)
			continue
		}
		if want := (m.Vec2{X: view.scaled, Y: view.scaled}); drawnView.scale != want {
			t.Errorf("a pixel-perfect camera %s draws %v pixels a unit, want %v", view.name, drawnView.scale, want)
		}
		wantDrawnRect := view.drawnRect
		if wantDrawnRect == (m.Recti{}) {
			wantDrawnRect = m.Recti{Width: view.width, Height: view.height}
		}
		if drawnView.drawnRect != wantDrawnRect {
			t.Errorf("a pixel-perfect camera %s draws its entries into %+v, want %+v", view.name, drawnView.drawnRect, wantDrawnRect)
		}
		if drawnView.snap != 1 {
			t.Errorf("a pixel-perfect camera %s snaps its entries to a grid of %v units, want 1", view.name, drawnView.snap)
		}
	}
}

func TestAPixelPerfectCameraPlacesTheWorldOnWholePixelsAndScrollsAPixelAtATime(t *testing.T) {
	// 320×180 art pixels expanded into 1001×721, three pixels a unit, the
	// viewport's centre at (500.5, 360.5): the world's origin lands half a
	// pixel up from where its position puts it, and a third of a unit
	// across moves it a pixel.
	targets := windowTargets{size: m.Vec2{X: 1001, Y: 721}, scale: 1}
	for _, placement := range []struct {
		name     string
		position m.Vec2
		origin   m.Vec2
	}{
		{"at the origin", m.Vec2{}, m.Vec2{X: 501, Y: 361}},
		{"at (0.3, -0.45)", m.Vec2{X: 0.3, Y: -0.45}, m.Vec2{X: 500, Y: 362}},
		{"at (0.3, -0.45) and a third across", m.Vec2{X: 0.3 + 1.0/3, Y: -0.45}, m.Vec2{X: 499, Y: 362}},
	} {
		camera := Camera{Size: m.Vec2{X: 320, Y: 180}, PixelPerfect: true, Position: placement.position}

		origin, _ := camera.WorldToScreen(m.Vec2{}, targets)
		corner, _ := camera.WorldToScreen(m.Vec2{X: 2, Y: -3}, targets)

		if !nearVec2(origin, placement.origin) {
			t.Errorf("a pixel-perfect camera %s shows the world's origin at %v, want %v", placement.name, origin, placement.origin)
		}
		if want := placement.origin.Add(m.Vec2{X: 6, Y: -9}); !nearVec2(corner, want) {
			t.Errorf("a pixel-perfect camera %s shows the world's (2, -3) at %v, want %v, whole pixels from its origin", placement.name, corner, want)
		}
	}
}

func TestACameraCullsTheEntriesWhoseBoundsMissItsViewOrTheirClipsPartOfIt(t *testing.T) {
	// A camera into 200×100 pixels at a unit a pixel sees x from -100 to
	// 100 and y from -50 to 50; turned an eighth, the bounding box of its
	// view reaches 75√2, about 106, from the origin along each axis. At
	// zoom 3 it sees x to 33⅓, and a pixel-perfect camera snaps an entry's
	// first corner to a whole unit, half up.
	plain := Camera{}
	turned := Camera{Rotation: math.Pi / 4}
	zoomed := Camera{Zoom: 3}
	pixelPerfect := Camera{PixelPerfect: true, Zoom: 3}
	inside := m.Rect{X: -10, Y: -10, Width: 20, Height: 20}
	for _, seen := range []struct {
		name   string
		camera Camera
		shape  Shape
		want   bool
	}{
		{"a fill inside", plain, Shape{Rect: inside}, true},
		{"a fill right of the view", plain, Shape{Rect: m.Rect{X: 101, Y: -10, Width: 20, Height: 20}}, false},
		{"a fill across the view's left edge", plain, Shape{Rect: m.Rect{X: -110, Y: -10, Width: 20, Height: 20}}, true},
		{"a fill below the view moved into it", plain, Shape{Rect: m.Rect{Y: 60, Width: 10, Height: 10}, Transform: m.Translation2x3(0, -30)}, true},
		{"a fill moved out of the view", plain, Shape{Rect: m.Rect{Width: 10, Height: 10}, Transform: m.Translation2x3(0, 60)}, false},
		// Centred 112 right, its corner reaches 112 - 10√2, about 98.
		{"a fill turned an eighth right of the view, its corner reaching into it", plain, Shape{Rect: inside, Transform: m.Translation2x3(112, 0).Mul(m.Rotation2x3(math.Pi / 4))}, true},
		{"a stroke above the view", plain, Shape{Kind: Stroke, Rect: m.Rect{X: -10, Y: -80, Width: 20, Height: 20}, Width: 2}, false},
		{"a line below the view, its width reaching into it", plain, Shape{Kind: Line, From: m.Vec2{X: -10, Y: 55}, To: m.Vec2{X: 10, Y: 55}, Width: 12}, true},
		{"a hairline below the view", plain, Shape{Kind: Line, From: m.Vec2{X: -10, Y: 52}, To: m.Vec2{X: 10, Y: 52}}, false},
		{"a fill inside, its clip outside the view", plain, Shape{Rect: inside, Clip: m.Rect{X: 150, Width: 10, Height: 10}}, false},
		{"a fill inside, its clip in the view beside it", plain, Shape{Rect: inside, Clip: m.Rect{X: 50, Width: 10, Height: 10}}, false},
		{"a fill inside, across its clip", plain, Shape{Rect: inside, Clip: m.Rect{Width: 50, Height: 50}}, true},
		{"a fill below a turned camera's view, in its bounding box", turned, Shape{Rect: m.Rect{X: -5, Y: 80, Width: 10, Height: 10}}, true},
		{"a fill below a turned camera's bounding box", turned, Shape{Rect: m.Rect{X: -5, Y: 110, Width: 10, Height: 10}}, false},
		{"a fill a fifteenth of a unit right of a zoomed view", zoomed, Shape{Rect: m.Rect{X: 33.4, Width: 2, Height: 2}}, false},
		{"a fill a fifteenth of a unit right of a pixel-perfect view, snapped into it", pixelPerfect, Shape{Rect: m.Rect{X: 33.4, Width: 2, Height: 2}}, true},
		{"a fill two thirds of a unit right of a pixel-perfect view, snapped further right", pixelPerfect, Shape{Rect: m.Rect{X: 34, Width: 2, Height: 2}}, false},
	} {
		view, ok := makeCameraView(&seen.camera, 200, 100, 1)
		if !ok {
			t.Fatalf("%s: the camera has no view", seen.name)
		}
		visible := view.appendVisibleEntries(nil, []sortedEntry{{entryIndex: 0}}, []batchKey{{clip: seen.shape.Clip}}, []m.Rect{seen.shape.findBounds()})
		if got := len(visible) == 1; got != seen.want {
			t.Errorf("%s, %+v, is seen: %v, want %v", seen.name, seen.shape, got, seen.want)
		}
	}
}

func TestACamerasVisibleEntriesAreAppendedInOrderAfterThoseBefore(t *testing.T) {
	inside := Shape{Rect: m.Rect{Width: 10, Height: 10}}
	outside := Shape{Rect: m.Rect{X: 500, Width: 10, Height: 10}}
	shapes := []Shape{inside, outside, inside, outside, inside}
	var bounds []m.Rect
	for index := range shapes {
		bounds = append(bounds, shapes[index].findBounds())
	}
	state := newShapeFrame(shapes, makeEntrySorter(nil))
	entries := state.sorter.entries
	camera := Camera{}
	view, _ := makeCameraView(&camera, 200, 100, 1)
	earlier := []sortedEntry{{entryIndex: 99}}

	got := view.appendVisibleEntries(earlier, entries[1:5], findShapeBatchKeys(state), bounds)

	if want := []sortedEntry{earlier[0], entries[2], entries[4]}; !slices.Equal(got, want) {
		t.Errorf("the run %v appended %v, want those inside the view after the entries before, in order, %v", entries[1:5], got, want)
	}
}

func TestAClipsScissorIsItsBoundingBoxInTheTargetsPixelsRoundedHalfUpWithinTheDrawnRect(t *testing.T) {
	// A camera into 200×100 pixels at zoom 2 draws the world's origin at
	// (100, 50), 2 pixels a unit.
	zoomed := Camera{Zoom: 2}
	for _, clip := range []struct {
		name   string
		camera Camera
		clip   m.Rect
		want   m.Recti
		drawn  bool
	}{
		{"inside the view", zoomed, m.Rect{X: -10, Y: -5, Width: 20, Height: 10}, m.Recti{X: 80, Y: 40, Width: 40, Height: 20}, true},
		{"its edges between pixels", zoomed, m.Rect{X: 0.2, Y: 0.3, Width: 10, Height: 10}, m.Recti{X: 100, Y: 51, Width: 20, Height: 20}, true},
		{"reaching past the view", zoomed, m.Rect{X: 40, Y: -100, Width: 100, Height: 200}, m.Recti{X: 180, Width: 20, Height: 100}, true},
		{"outside the view", zoomed, m.Rect{X: 60, Width: 10, Height: 10}, m.Recti{}, false},
		{"too thin to hold a pixel's centre", zoomed, m.Rect{X: 0.1, Width: 0.1, Height: 10}, m.Recti{}, false},
		// A quarter turn against the camera takes (x, y) to (y, -x).
		{"under a camera turned a quarter", Camera{Zoom: 2, Rotation: math.Pi / 2}, m.Rect{X: -10, Y: -5, Width: 20, Height: 10}, m.Recti{X: 90, Y: 30, Width: 20, Height: 40}, true},
		// 100 units square inscribed between bars 50 pixels wide.
		{"past an inscribed camera's bars", Camera{Size: m.Vec2{X: 100, Y: 100}, Fit: FitInscribe}, m.Rect{X: -100, Y: -10, Width: 200, Height: 20}, m.Recti{X: 50, Y: 40, Width: 100, Height: 20}, true},
	} {
		view, ok := makeCameraView(&clip.camera, 200, 100, 1)
		if !ok {
			t.Fatalf("%s: the camera has no view", clip.name)
		}
		got, drawn := view.placeClip(clip.clip)
		if got != clip.want || drawn != clip.drawn {
			t.Errorf("a clip %s, %+v, is scissored to %+v, drawn: %v, want %+v, %v", clip.name, clip.clip, got, drawn, clip.want, clip.drawn)
		}
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
