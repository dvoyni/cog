package canvas_test

import (
	"encoding/binary"
	"image/color"
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
			white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			picture.expect(t, []probe{
				{"the left half's fill", m.Vec2{X: -110}, white},
				{"the left half where the right's layer would show", m.Vec2{X: -50}, greenShown},
				{"the left half's corner", m.Vec2{X: -155, Y: -115}, greenShown},
				{"the left half by the middle", m.Vec2{X: -2, Y: 100}, greenShown},
				{"the right half's fill", m.Vec2{X: 110}, white},
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
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for _, inscribed := range []struct {
		name     string
		viewport m.Rect
		probes   []probe
	}{
		{"into the whole window", m.Rect{}, []probe{
			{"the left bar", m.Vec2{X: -150}, greenShown},
			{"the left bar by the logical size", m.Vec2{X: -122, Y: 100}, greenShown},
			{"the logical size by the left bar", m.Vec2{X: -118, Y: 100}, white},
			{"the middle", m.Vec2{}, white},
			{"the logical size by the right bar", m.Vec2{X: 118, Y: -100}, white},
			{"the right bar by the logical size", m.Vec2{X: 122, Y: -100}, greenShown},
			{"the right bar", m.Vec2{X: 150}, greenShown},
		}},
		{"into the left half", m.Rect{Width: 0.5, Height: 1}, []probe{
			{"the top bar", m.Vec2{X: -80, Y: -110}, greenShown},
			{"the top bar by the logical size", m.Vec2{X: -150, Y: -82}, greenShown},
			{"the logical size by the top bar", m.Vec2{X: -150, Y: -78}, white},
			{"the middle", m.Vec2{X: -80}, white},
			{"the logical size by the bottom bar", m.Vec2{X: -10, Y: 78}, white},
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
			block := findPassParameter(t, pass.Draws[0], "canvas_camera")
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

// minimapDescriptor is a small texture a camera draws into.
var minimapDescriptor = render.TextureDescriptor{Label: "minimap", Width: 16, Height: 16, Format: render.TextureFormatRGBA8Unorm}

// findPassParameter answers the bytes draw bound to its pass parameter name,
// failing t when it bound none.
func findPassParameter(t *testing.T, draw render.InspectedDraw, name string) []byte {
	t.Helper()
	index := slices.IndexFunc(draw.PassParameters, func(parameter render.InspectedParameter) bool { return parameter.Name == name })
	if index < 0 {
		t.Fatalf("the draw bound no pass parameter %q: %+v", name, draw.PassParameters)
	}
	return draw.PassParameters[index].Uniform
}
