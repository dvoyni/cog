package canvas_test

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestTwoCamerasOfHalfViewportsEachClearOnlyTheirHalf(t *testing.T) {
	// Each camera clears its half and draws its own layer's white fill, the
	// left's 30 units left of its viewport's centre and the right's 30
	// right: a camera clearing past its half would wipe the fill of the one
	// set before it, whichever that is, and one drawing the other's layer
	// would show its fill too.
	left := canvas.Camera{Layers: canvas.MakeLayer(0), Viewport: m.Rect{Width: 0.5, Height: 1}, Clear: true, ClearColor: green}
	right := canvas.Camera{Layers: canvas.MakeLayer(1), Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Clear: true, ClearColor: orange}
	for _, setting := range []struct {
		name    string
		cameras []canvas.Camera
	}{
		{"the left set first", []canvas.Camera{left, right}},
		{"the right set first", []canvas.Camera{right, left}},
	} {
		t.Run(setting.name, func(t *testing.T) {
			picture := captureDrawing(t, func(list canvas.DrawList) {
				for _, camera := range setting.cameras {
					list.SetCamera(camera)
				}
				list.DrawShape(canvas.Shape{Rect: m.Rect{X: -40, Y: -10, Width: 20, Height: 20}, Tint: m.White})
				list.DrawShape(canvas.Shape{Rect: m.Rect{X: 20, Y: -10, Width: 20, Height: 20}, Tint: m.White, Layer: 1})
			})

			// The probes are in the window's points from its centre.
			picture.expect(t, []probe{
				{"the left half's fill", m.Vec2{X: -110}, opaqueWhite},
				{"the left half where the right's layer would show", m.Vec2{X: -50}, greenShown},
				{"the left half's corner", m.Vec2{X: -155, Y: -115}, greenShown},
				{"the left half by the middle", m.Vec2{X: -2, Y: 100}, greenShown},
				{"the right half's fill", m.Vec2{X: 110}, opaqueWhite},
				{"the right half where the left's layer would show", m.Vec2{X: 50}, orangeShown},
				{"the right half's corner", m.Vec2{X: 155, Y: 115}, orangeShown},
				{"the right half by the middle", m.Vec2{X: 2, Y: -100}, orangeShown},
			})
		})
	}
}

func TestAnInscribedCamerasBarsShowItsClearColourAndItsEntriesDrawOnlyBetweenThem(t *testing.T) {
	// A logical size 100 units square inscribed, its white fill reaching far
	// past it: into the whole window of 320×240 points, 240 points square
	// between bars 40 wide left and right, the viewport cleared by the
	// pass's load; and into the left half, 160 points square between bars
	// 40 tall above and below, the clear drawn over it. The probes are in
	// the window's points from its centre.
	for _, inscribed := range []struct {
		name     string
		viewport m.Rect
		probes   []probe
	}{
		{"into the whole window", m.Rect{}, []probe{
			{"the left bar", m.Vec2{X: -150}, greenShown},
			{"the left bar by the logical size", m.Vec2{X: -122, Y: 100}, greenShown},
			{"the logical size by the left bar", m.Vec2{X: -118, Y: 100}, opaqueWhite},
			{"the middle", m.Vec2{}, opaqueWhite},
			{"the logical size by the right bar", m.Vec2{X: 118, Y: -100}, opaqueWhite},
			{"the right bar by the logical size", m.Vec2{X: 122, Y: -100}, greenShown},
			{"the right bar", m.Vec2{X: 150}, greenShown},
		}},
		{"into the left half", m.Rect{Width: 0.5, Height: 1}, []probe{
			{"the top bar", m.Vec2{X: -80, Y: -110}, greenShown},
			{"the top bar by the logical size", m.Vec2{X: -150, Y: -82}, greenShown},
			{"the logical size by the top bar", m.Vec2{X: -150, Y: -78}, opaqueWhite},
			{"the middle", m.Vec2{X: -80}, opaqueWhite},
			{"the logical size by the bottom bar", m.Vec2{X: -10, Y: 78}, opaqueWhite},
			{"the bottom bar by the logical size", m.Vec2{X: -10, Y: 82}, greenShown},
			{"the bottom bar", m.Vec2{X: -80, Y: 110}, greenShown},
		}},
	} {
		t.Run(inscribed.name, func(t *testing.T) {
			picture := captureDrawing(t, func(list canvas.DrawList) {
				list.SetCamera(canvas.Camera{Viewport: inscribed.viewport, Size: m.Vec2{X: 100, Y: 100}, Fit: canvas.FitInscribe, Clear: true, ClearColor: green})
				list.DrawShape(canvas.Shape{Rect: m.Rect{X: -1000, Y: -1000, Width: 2000, Height: 2000}, Tint: m.White})
			})

			picture.expect(t, inscribed.probes)
		})
	}
}

func TestAHairlineIsOneTargetPixelWideAtTwoCameraZooms(t *testing.T) {
	// Each half of the window is a camera's, at zoom 1 or 3, cleared to
	// black, drawing a hairline stroke 120 points square about its
	// viewport's centre and two hairlines 70.5 points from it, on a
	// pixel's middle, each the same in the window's points whatever the
	// zoom: world units are points over the zoom.
	halves := []struct {
		name   string
		camera canvas.Camera
		centre m.Vec2
	}{
		{"at zoom 1", canvas.Camera{Layers: canvas.MakeLayer(0), Viewport: m.Rect{Width: 0.5, Height: 1}, Zoom: 1, Clear: true, ClearColor: m.Color{A: 1}}, m.Vec2{X: -80}},
		{"at zoom 3", canvas.Camera{Layers: canvas.MakeLayer(1), Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Zoom: 3, Clear: true, ClearColor: m.Color{A: 1}}, m.Vec2{X: 80}},
	}
	picture := captureDrawing(t, func(list canvas.DrawList) {
		for _, half := range halves {
			list.SetCamera(half.camera)
			unit := 1 / half.camera.Zoom
			layer := half.camera.Layers[0].From
			list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: m.Rect{X: -60 * unit, Y: -60 * unit, Width: 120 * unit, Height: 120 * unit}, Tint: m.White, Layer: layer})
			list.DrawShape(canvas.Shape{Kind: canvas.Line, From: m.Vec2{X: -60 * unit, Y: 70.5 * unit}, To: m.Vec2{X: 60 * unit, Y: 70.5 * unit}, Tint: m.White, Layer: layer})
			list.DrawShape(canvas.Shape{Kind: canvas.Line, From: m.Vec2{X: 70.5 * unit, Y: -60 * unit}, To: m.Vec2{X: 70.5 * unit, Y: 60 * unit}, Tint: m.White, Layer: layer})
		}
	})

	for _, half := range halves {
		at := func(x, y float32) m.Vec2 { return half.centre.Add(m.Vec2{X: x, Y: y}) }
		for _, crossing := range []struct {
			name     string
			from, to m.Vec2
		}{
			{"the stroke's top side", at(0, -65), at(0, -55)},
			{"the stroke's bottom side", at(0, 55), at(0, 65)},
			{"the stroke's left side", at(-65, 0), at(-55, 0)},
			{"the stroke's right side", at(55, 0), at(65, 0)},
			{"the horizontal line", at(0, 66), at(0, 75)},
			{"the vertical line", at(66, 0), at(75, 0)},
		} {
			if lit := picture.countLit(crossing.from, crossing.to); lit != 1 {
				t.Errorf("%s, %s, is %d pixels wide from %v to %v, want 1", half.name, crossing.name, lit, crossing.from, crossing.to)
			}
		}
	}
}

func TestAPixelPerfectCamerasEntriesLandOnTheWorldUnitGridWholePixelsApart(t *testing.T) {
	// 80×60 art pixels expanded into the window of 320×240 points, four
	// points a unit, cleared to black: a fill at a fractional position, an
	// odd-sized one centred on a fractional position, and two odd-sized
	// ones centred on whole units one apart, each drawn where its first
	// corner rounds to, half up, on the world-unit grid.
	fills := []struct {
		name          string
		drawn, landed m.Rect
	}{
		{"a fill at a fractional position", m.Rect{X: 10.3, Y: -5.6, Width: 3, Height: 3}, m.Rect{X: 10, Y: -6, Width: 3, Height: 3}},
		{"an odd-sized fill centred on (-20.2, 10.3)", m.Rect{X: -20.2 - 2.5, Y: 10.3 - 2.5, Width: 5, Height: 5}, m.Rect{X: -23, Y: 8, Width: 5, Height: 5}},
		{"an odd-sized fill centred on x 20", m.Rect{X: 20 - 2.5, Y: -12, Width: 5, Height: 3}, m.Rect{X: 18, Y: -12, Width: 5, Height: 3}},
		{"an odd-sized fill centred on x 21", m.Rect{X: 21 - 2.5, Y: -7, Width: 5, Height: 3}, m.Rect{X: 19, Y: -7, Width: 5, Height: 3}},
	}
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Size: m.Vec2{X: 80, Y: 60}, PixelPerfect: true, Clear: true, ClearColor: m.Color{A: 1}})
		for _, fill := range fills {
			list.DrawShape(canvas.Shape{Rect: fill.drawn, Tint: m.White})
		}
	})

	// Each edge is probed half a point either side, in the window's points
	// from its centre, four a unit.
	for _, fill := range fills {
		landed := fill.landed
		left, top := landed.X*4, landed.Y*4
		right, bottom := (landed.X+landed.Width)*4, (landed.Y+landed.Height)*4
		middle := m.Vec2{X: (left + right) / 2, Y: (top + bottom) / 2}
		picture.expect(t, []probe{
			{fill.name + ", inside its left edge", m.Vec2{X: left + 0.5, Y: middle.Y}, opaqueWhite},
			{fill.name + ", outside its left edge", m.Vec2{X: left - 0.5, Y: middle.Y}, opaqueBlack},
			{fill.name + ", inside its right edge", m.Vec2{X: right - 0.5, Y: middle.Y}, opaqueWhite},
			{fill.name + ", outside its right edge", m.Vec2{X: right + 0.5, Y: middle.Y}, opaqueBlack},
			{fill.name + ", inside its top edge", m.Vec2{X: middle.X, Y: top + 0.5}, opaqueWhite},
			{fill.name + ", outside its top edge", m.Vec2{X: middle.X, Y: top - 0.5}, opaqueBlack},
			{fill.name + ", inside its bottom edge", m.Vec2{X: middle.X, Y: bottom - 0.5}, opaqueWhite},
			{fill.name + ", outside its bottom edge", m.Vec2{X: middle.X, Y: bottom + 0.5}, opaqueBlack},
		})
	}
}

func TestAPixelPerfectCamerasStretchIsReportedOnce(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindPixelPerfectStretch)

	for range 3 {
		h.Record(func(list canvas.DrawList) {
			list.SetCamera(canvas.Camera{Size: m.Vec2{X: 16, Y: 16}, Fit: canvas.FitStretch, PixelPerfect: true, Clear: true})
		})
		h.Step()
	}

	if reports := h.Reports(); len(reports) != 1 || reports[0].Kind != canvas.ReportKindPixelPerfectStretch {
		t.Errorf("three frames of a pixel-perfect camera stretching reported %v, want one report of it", reports)
	}
	if frames := h.Frames(); len(frames[len(frames)-1].Cameras) != 1 {
		t.Errorf("the last frame was seen through %+v, want the camera drawn", frames[len(frames)-1].Cameras)
	}
}

func TestAClippedEntryDrawsNothingOutsideItsClipInEachOfTwoViewports(t *testing.T) {
	// A white fill far larger than the window, clipped to 40×20 units about
	// the world's origin, seen by two cameras cleared to green: the left
	// half's, at the origin and zoom 1, a unit a point, its viewport's
	// centre 80 points left of the window's; and the right half's, at
	// (10, 5) and zoom 2, its centre 80 points right. The clip lies from
	// (-100, -10) to (-60, 10) in the window's points from its centre
	// through the left camera, and from (20, -30) to (100, 10) through the
	// right.
	clip := m.Rect{X: -20, Y: -10, Width: 40, Height: 20}
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Viewport: m.Rect{Width: 0.5, Height: 1}, Clear: true, ClearColor: green})
		list.SetCamera(canvas.Camera{Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Position: m.Vec2{X: 10, Y: 5}, Zoom: 2, Clear: true, ClearColor: green})
		list.DrawShape(canvas.Shape{Rect: m.Rect{X: -1000, Y: -1000, Width: 2000, Height: 2000}, Tint: m.White, Clip: clip})
	})

	picture.expect(t, []probe{
		{"the left clip's middle", m.Vec2{X: -80}, opaqueWhite},
		{"the left clip's top left corner", m.Vec2{X: -98, Y: -8}, opaqueWhite},
		{"the left clip's bottom right corner", m.Vec2{X: -62, Y: 8}, opaqueWhite},
		{"left of the left clip", m.Vec2{X: -102}, greenShown},
		{"right of the left clip", m.Vec2{X: -58}, greenShown},
		{"above the left clip", m.Vec2{X: -80, Y: -12}, greenShown},
		{"below the left clip", m.Vec2{X: -80, Y: 12}, greenShown},
		{"the right clip's middle", m.Vec2{X: 60, Y: -10}, opaqueWhite},
		{"the right clip's top left corner", m.Vec2{X: 22, Y: -28}, opaqueWhite},
		{"the right clip's bottom right corner", m.Vec2{X: 98, Y: 8}, opaqueWhite},
		{"left of the right clip", m.Vec2{X: 18, Y: -10}, greenShown},
		{"right of the right clip", m.Vec2{X: 102, Y: -10}, greenShown},
		{"above the right clip", m.Vec2{X: 60, Y: -32}, greenShown},
		{"below the right clip", m.Vec2{X: 60, Y: 12}, greenShown},
	})
}

func TestAnEntryOutsideOneCamerasViewIsCulledByItAndDrawnByAnother(t *testing.T) {
	// The window's halves, each a camera's that clears, the left at the
	// world's origin and the right at x 1000, and a camera of the whole
	// window at x -1000, at order 1, that does not clear: a fill at x 1000
	// only the right one sees.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	fill := canvas.Shape{Rect: m.Rect{X: 995, Y: -5, Width: 10, Height: 10}, Tint: m.White}
	h.Record(func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Viewport: m.Rect{Width: 0.5, Height: 1}, Clear: true})
		list.SetCamera(canvas.Camera{Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Position: m.Vec2{X: 1000}, Clear: true})
		list.SetCamera(canvas.Camera{Position: m.Vec2{X: -1000}, Order: 1})
		list.DrawShape(fill)
	})

	h.Step()

	frame := h.Frames()[0]
	if len(frame.Cameras) != 2 {
		t.Fatalf("the frame was seen through %+v, want the two halves' cameras alone, the one culling the fill and not clearing drawing nothing", frame.Cameras)
	}
	if batches := frame.Cameras[0].Batches; len(batches) != 0 {
		t.Errorf("the left camera, at the origin, drew %+v, want the fill culled", batches)
	}
	if batches := frame.Cameras[1].Batches; len(batches) != 1 || len(batches[0].Entries) != 1 || batches[0].Entries[0].Shape != fill {
		t.Errorf("the right camera, at x 1000, drew %+v, want one batch of the fill", batches)
	}
	lists := findCanvasLists(frame.Render)
	if len(lists) != 1 || len(lists[0].Passes) != 2 {
		t.Fatalf("canvas began %+v, want one list at order 0 of the halves' two passes", lists)
	}
	if left, right := len(lists[0].Passes[0].Draws), len(lists[0].Passes[1].Draws); left != 1 || right != 2 {
		t.Errorf("the left pass made %d draws and the right %d, want the left's clear alone, and the right's clear and its fill", left, right)
	}
}

func TestCamerasAtTwoOrdersBeginAListAtEachAmongAnotherPluginsAndACameraAtATakenOrderIsReported(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &orderTaker{order: 3})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Harness.ExpectReport(render.ReportKindOrderTaken)
	h.Record(func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Order: 10, Clear: true})
		list.SetCamera(canvas.Camera{Order: 3, Clear: true})
		list.SetCamera(canvas.Camera{Order: -5, Clear: true})
	})

	h.Step()

	frame := h.Frames()[0]
	var orders []render.Order
	for _, camera := range frame.Cameras {
		orders = append(orders, camera.Camera.Order)
	}
	if want := []render.Order{-5, 3, 10}; !slices.Equal(orders, want) {
		t.Errorf("the cameras drew at orders %v, want %v, in order", orders, want)
	}
	var labels []string
	for _, list := range frame.Render.CommandLists {
		if list.Label == "canvas" || list.Label == "taken" {
			labels = append(labels, list.Label)
		}
	}
	if want := []string{"canvas", "taken", "canvas"}; !slices.Equal(labels, want) {
		t.Errorf("the frame's lists are %v, want canvas's at -5 and 10 about the order taker's at 3, %v, the camera at 3 refused", labels, want)
	}
	reports := h.Harness.Reports()
	if len(reports) != 1 || reports[0].Kind != render.ReportKindOrderTaken {
		t.Errorf("render reported %v, want the camera's list at the taken order 3 alone", reports)
	}
}

func TestAtEqualOrdersACameraIntoATextureDrawsBeforeOneIntoAWindowSetBeforeIt(t *testing.T) {
	h := canvastest.NewHarness(t)
	target := &textureCamera{descriptor: minimapDescriptor, camera: canvas.Camera{Clear: true}}
	h.Spawn(nil, nil, target)
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	// canvastest's draw list comes before the texture camera's.
	h.Record(func(list canvas.DrawList) { list.SetCamera(canvas.Camera{Clear: true}) })

	h.Step()

	lists := findCanvasLists(h.Frames()[0].Render)
	if len(lists) != 1 || len(lists[0].Passes) != 2 {
		t.Fatalf("canvas began %+v, want one list of two passes for the two cameras at order 0", lists)
	}
	passes := lists[0].Passes
	if passes[0].ColorTargets[0].Texture != target.madeTexture() || passes[1].ColorTargets[0].Window != window {
		t.Errorf("the passes draw into %+v then %+v, want the texture %v first, then the window %v", passes[0].ColorTargets, passes[1].ColorTargets, target.madeTexture(), window)
	}
}

func TestATextureTargetsViewportIsPlacedFromItsDescriptorAndItsOutputEncodedAsItsFormatAsks(t *testing.T) {
	for _, format := range []struct {
		name   string
		format render.TextureFormat
		encode uint32
	}{
		{"of a format encoding nothing", render.TextureFormatRGBA8Unorm, 1},
		{"of an sRGB format", render.TextureFormatRGBA8UnormSrgb, 0},
	} {
		t.Run(format.name, func(t *testing.T) {
			h := canvastest.NewHarness(t)
			target := &textureCamera{
				descriptor: render.TextureDescriptor{Label: "minimap", Width: 64, Height: 32, Format: format.format},
				camera:     canvas.Camera{Viewport: m.Rect{X: 0.25, Y: 0.5, Width: 0.5, Height: 0.5}, Clear: true},
			}
			h.Spawn(nil, nil, target)
			h.OpenWindow(platform.Size{Width: 64, Height: 48})

			h.Step()

			frame := h.Frames()[0]
			wantViewport := m.Recti{X: 16, Y: 16, Width: 32, Height: 16}
			if len(frame.Cameras) != 1 || frame.Cameras[0].Camera.Texture != target.madeTexture() || frame.Cameras[0].Viewport != wantViewport {
				t.Fatalf("the frame was seen through %+v, want the texture camera's alone, into %v over %v", frame.Cameras, target.madeTexture(), wantViewport)
			}
			lists := findCanvasLists(frame.Render)
			if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 1 {
				t.Fatalf("canvas began %+v, want one list of one pass with one draw, the viewport's clear", lists)
			}
			pass := lists[0].Passes[0]
			if pass.ColorTargets[0].Clear {
				t.Errorf("the pass clears its whole target by its load, want the viewport's clear drawn over it alone")
			}
			block := findParameter(t, pass.Draws[0].PassParameters, "canvas_camera").Uniform
			// CanvasCamera's pixel_size is at byte 48, viewport_origin at
			// 56, viewport_size at 64 and encode at 72.
			readFloat := func(offset int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(block[offset:])) }
			got := []float32{readFloat(48), readFloat(56), readFloat(60), readFloat(64), readFloat(68)}
			if want := []float32{1, 16, 16, 32, 16}; !slices.Equal(got, want) {
				t.Errorf("the camera's pixel size, viewport origin and size are %v, want %v: a texel a world unit, the viewport in the texture's pixels", got, want)
			}
			if encode := binary.LittleEndian.Uint32(block[72:]); encode != format.encode {
				t.Errorf("the camera's encode is %d, want %d", encode, format.encode)
			}
		})
	}
}

func TestACameraThatClearsDrawsInAFrameWithNoEntriesItsClearEncodedAndOneThatDoesNotDrawsNothing(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Clear: true, ClearColor: m.NewColorSrgb(0.5, 0.25, 1, 1)})
	})
	h.Step()
	h.Record(func(list canvas.DrawList) { list.SetCamera(canvas.Camera{}) })
	h.Step()

	frames := h.Frames()
	lists := findCanvasLists(frames[0].Render)
	if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 0 {
		t.Fatalf("a frame of a clearing camera alone began %+v, want one list of one pass drawing nothing", lists)
	}
	target := lists[0].Passes[0].ColorTargets[0]
	clearColor := []float64{target.ClearColor.R, target.ClearColor.G, target.ClearColor.B, target.ClearColor.A}
	for index, want := range []float64{0.5, 0.25, 1, 1} {
		if math.Abs(clearColor[index]-want) > 1e-5 {
			t.Errorf("the pass clears to %v, want the clear colour encoded as sRGB, as the window's format does not, %v", target.ClearColor, []float64{0.5, 0.25, 1, 1})
			break
		}
	}
	if target.Window != window || !target.Clear {
		t.Errorf("the pass targets %+v, want the main window, %v, cleared by its load", target, window)
	}
	if lists := findCanvasLists(frames[1].Render); len(lists) != 0 || len(frames[1].Cameras) != 0 {
		t.Errorf("a frame of a camera that does not clear, with no entries, began %+v and was seen through %+v, want nothing", lists, frames[1].Cameras)
	}
}

func TestACameraWhoseTargetIsRefusedIsSkippedAndReportedOnce(t *testing.T) {
	for _, refusal := range []struct {
		name          string
		namesWindow   bool
		releasesFirst bool
	}{
		{"naming a window and a texture", true, false},
		{"into a texture render does not hold", false, true},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			h := canvastest.NewHarness(t)
			target := &textureCamera{descriptor: minimapDescriptor, releases: refusal.releasesFirst}
			h.Spawn(nil, nil, target)
			window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
			target.mutex.Lock()
			target.camera = canvas.Camera{Clear: true}
			if refusal.namesWindow {
				target.camera.Window = window
			}
			target.mutex.Unlock()
			h.ExpectReport(canvas.ReportKindCameraTargetRefused)

			// A texture released is held until the frame it was released
			// in ends.
			for range 3 {
				h.Step()
			}

			frames := h.Frames()
			if last := frames[len(frames)-1]; len(last.Cameras) != 0 || len(findCanvasLists(last.Render)) != 0 {
				t.Errorf("the last frame was seen through %+v, want the camera skipped", last.Cameras)
			}
			if reports := h.Reports(); len(reports) != 1 {
				t.Errorf("three frames of the camera reported %v, want one report", reports)
			}
		})
	}
}

func TestAMultisampledCameraDrawsIntoAFourSampleTextureOfItsViewportResolvedIntoOneOfOneAndDrawnOnceOverIt(t *testing.T) {
	// The window's halves, each a 4-sample camera's over a fill across the
	// window: the left clears to white, and the right does not.
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	h.Spawn(nil, nil, describer)
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	record := func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Viewport: m.Rect{Width: 0.5, Height: 1}, Clear: true, ClearColor: m.White, Samples: 4})
		list.SetCamera(canvas.Camera{Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Samples: 4})
		list.DrawShape(canvas.Shape{Rect: m.Rect{X: -100, Y: -100, Width: 200, Height: 200}, Tint: green})
	}
	h.Record(record)

	h.Step()

	lists := findCanvasLists(h.Frames()[0].Render)
	if len(lists) != 1 || len(lists[0].Passes) != 4 {
		t.Fatalf("canvas began %+v, want one list of four passes, each camera's entries and then what they resolved into drawn over its viewport", lists)
	}
	// The inspection spells each factor at its default, One as a source,
	// zero.
	over := render.BlendComponent{DestinationFactor: render.BlendFactorOneMinusSourceAlpha}
	var textures []render.Texture
	for index, half := range []struct {
		name       string
		clearColor render.Color
		blend      render.BlendState
	}{
		{"the left camera, which clears", render.Color{R: 1, G: 1, B: 1, A: 1}, render.BlendState{}},
		{"the right camera, which does not", render.Color{}, render.BlendState{Color: over, Alpha: over}},
	} {
		entriesPass := lists[0].Passes[2*index]
		drawnPass := lists[0].Passes[2*index+1]
		if len(entriesPass.ColorTargets) != 1 || len(entriesPass.Draws) != 1 || len(drawnPass.Draws) != 1 {
			t.Fatalf("%s drew its entries in %+v and then %+v, want one target and the fill's batch, then one draw", half.name, entriesPass, drawnPass)
		}
		multisampled := entriesPass.ColorTargets[0]
		resolved := multisampled.Resolve.Texture
		if multisampled.Texture == (render.Texture{}) || multisampled.Window != (platform.Window{}) || !multisampled.Clear || multisampled.ClearColor != half.clearColor || !multisampled.Discard || resolved == (render.Texture{}) {
			t.Errorf("%s drew its entries into %+v, want a texture cleared to %v by its load, discarded, and resolved into another", half.name, multisampled, half.clearColor)
		}
		block := findParameter(t, entriesPass.Draws[0].PassParameters, "canvas_camera").Uniform
		readFloat := func(offset int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(block[offset:])) }
		if got, want := []float32{readFloat(56), readFloat(60), readFloat(64), readFloat(68)}, []float32{0, 0, 32, 48}; !slices.Equal(got, want) {
			t.Errorf("%s's viewport origin and size, as its entries drew, are %v, want %v, the whole of a texture of the viewport's size", half.name, got, want)
		}
		if want := []render.ColorTarget{{Window: window}}; !slices.Equal(drawnPass.ColorTargets, want) {
			t.Errorf("%s drew what it resolved into %+v, want %+v, the window as it holds it", half.name, drawnPass.ColorTargets, want)
		}
		draw := drawnPass.Draws[0]
		if draw.Material.VertexEntryPoint != "canvas_viewport_vertex" || draw.VertexCount != 6 || draw.InstanceCount != 1 {
			t.Errorf("%s drew what it resolved through %q, %d vertices of %d instances, want canvas_viewport_vertex, 6 of 1", half.name, draw.Material.VertexEntryPoint, draw.VertexCount, draw.InstanceCount)
		}
		if blend := draw.Material.State.ColorTargets[0].Blend; blend != half.blend {
			t.Errorf("%s drew what it resolved blended by %+v, want %+v", half.name, blend, half.blend)
		}
		if atlas := findParameter(t, draw.DrawParameters, "canvas_atlas"); atlas.TextureView.Texture != resolved {
			t.Errorf("%s drew %v over its viewport, want %v, what it resolved into", half.name, atlas.TextureView.Texture, resolved)
		}
		if encode := binary.LittleEndian.Uint32(findParameter(t, draw.PassParameters, "canvas_camera").Uniform[72:]); encode != 0 {
			t.Errorf("%s encoded what it resolved, already encoded, again, by %d", half.name, encode)
		}
		textures = append(textures, multisampled.Texture, resolved)
	}

	describer.describe(textures...)
	h.Record(record)
	h.Step()

	multisampled := render.TextureDescriptor{Label: "canvas multisampled camera", Width: 32, Height: 48, LayerCount: 1, MipLevelCount: 1, SampleCount: 4, Format: render.TextureFormatBGRA8Unorm}
	resolved := multisampled
	resolved.Label = "canvas resolved camera"
	resolved.SampleCount = 1
	if got, want := describer.describedTextures(), []render.TextureDescriptor{multisampled, resolved, multisampled, resolved}; !slices.Equal(got, want) {
		t.Errorf("the cameras' textures are %+v, want %+v, each of its viewport's size in the window's format", got, want)
	}
}

func TestAMultisampledCameraClearingTheWholeWindowDrawsWhatItResolvedOverTheWindowClearedByTheLoad(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.SetCamera(canvas.Camera{Clear: true, Samples: 4})
	})

	h.Step()

	lists := findCanvasLists(h.Frames()[0].Render)
	if len(lists) != 1 || len(lists[0].Passes) != 2 {
		t.Fatalf("canvas began %+v, want one list of the camera's entries' pass and its composite", lists)
	}
	// What the camera resolved replaces the whole window, so the window
	// need not be read first.
	if got, want := lists[0].Passes[1].ColorTargets, []render.ColorTarget{{Window: window, Clear: true, ClearColor: render.Color{}}}; !slices.Equal(got, want) {
		t.Errorf("the composite drew into %+v, want %+v", got, want)
	}
}

func TestACameraOfASampleCountOtherThanOneOrFourPanicsAsItIsSet(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	var recovered any
	h.Record(func(list canvas.DrawList) {
		defer func() { recovered = recover() }()
		list.SetCamera(canvas.Camera{Samples: 2})
	})

	h.Step()

	if recovered == nil {
		t.Error("setting a camera of 2 samples did not panic")
	}
}

// minimapDescriptor is a small texture a camera draws into.
var minimapDescriptor = render.TextureDescriptor{Label: "minimap", Width: 16, Height: 16, Format: render.TextureFormatRGBA8Unorm}

// findParameter answers the parameter of parameters, a draw's pass or draw
// parameters, named name, failing t when there is none.
func findParameter(t *testing.T, parameters []render.InspectedParameter, name string) render.InspectedParameter {
	t.Helper()
	index := slices.IndexFunc(parameters, func(parameter render.InspectedParameter) bool { return parameter.Name == name })
	if index < 0 {
		t.Fatalf("the draw bound no parameter %q: %+v", name, parameters)
	}
	return parameters[index]
}
