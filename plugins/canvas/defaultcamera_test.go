package canvas_test

import (
	"image/color"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestAFrameOfFillsReachesRenderAsOneListAtOrderZeroOfOnePassAndOneInstancedDraw(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, listBracketer{})
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		for index := range 3 {
			list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: float32(index) * 8, Width: 4, Height: 4}, Tint: m.White})
		}
	})

	h.Step()

	frames := h.Frames()
	lists := slices.DeleteFunc(frames[len(frames)-1].Render.CommandLists, func(list render.InspectedCommandList) bool {
		return list.Label != "before" && list.Label != "canvas" && list.Label != "after"
	})
	var labels []string
	for _, list := range lists {
		labels = append(labels, list.Label)
	}
	if want := []string{"before", "canvas", "after"}; !slices.Equal(labels, want) {
		t.Fatalf("the frame's lists are %v, want canvas's one between orders -1 and 1: %v", labels, want)
	}
	passes := lists[1].Passes
	if len(passes) != 1 {
		t.Fatalf("canvas's list holds %d passes, want the default camera's one", len(passes))
	}
	if want := []render.ColorTarget{{Window: window, Clear: true, ClearColor: render.Color{A: 1}}}; !reflect.DeepEqual(passes[0].ColorTargets, want) {
		t.Errorf("the default camera's pass targets %+v, want the window cleared to opaque black, %+v", passes[0].ColorTargets, want)
	}
	draws := passes[0].Draws
	if len(draws) != 1 {
		t.Fatalf("the default camera's pass holds %d draws, want one for the three fills", len(draws))
	}
	if draws[0].VertexCount != 6 || draws[0].InstanceCount != 3 || draws[0].FirstInstance != 0 {
		t.Errorf("the draw is of %d vertices, %d instances from instance %d, want 6 vertices, 3 instances from 0", draws[0].VertexCount, draws[0].InstanceCount, draws[0].FirstInstance)
	}
}

func TestAFillShowsItsTintPremultipliedAndEncodedAsSRGBInTheWindowThroughTheDefaultCamera(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.UseSoftwareAdapter()
	capturer := &windowCapturer{}
	h.Spawn(nil, nil, capturer, eventPump{})
	window := h.OpenWindow(platform.Size{Width: 320, Height: 240})
	// The default camera centres the world's origin in the window, a world
	// unit to a point and y down: the opaque fill covers the centre, the
	// translucent one, drawn after it, most of the top-left quarter and the
	// opaque fill's top-left corner, and nothing the bottom half.
	opaque := m.NewColorSrgb(50.0/255, 1, 150.0/255, 1)
	translucent := m.NewColorSrgb(1, 1.0/3, 0, 0.6)
	record := func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -20, Y: -20, Width: 40, Height: 40}, Tint: opaque})
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -150, Y: -110, Width: 145, Height: 105}, Tint: translucent})
	}

	// A Wayland window gets a framebuffer only once the compositor's frame
	// callback for its last present is read, a while after it, so the test
	// steps for a time, not a count of beats.
	capturer.capture(window)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		h.Record(record)
		h.Step()
		picture := capturer.capturedPicture()
		if picture == nil {
			continue
		}
		width := picture.Bounds().Dx()
		height := picture.Bounds().Dy()
		// overlap is 12 points up and left of the centre, in pixels.
		overlap := 12 * width / 320
		for _, probe := range []struct {
			name string
			x, y int
			want color.NRGBA
		}{
			{"the opaque fill's middle", width / 2, height / 2, color.NRGBA{R: 50, G: 255, B: 150, A: 255}},
			// 0.6 of the encoded (255, 85, 0), over opaque black.
			{"the translucent fill's middle", width / 4, height / 4, color.NRGBA{R: 153, G: 51, B: 0, A: 255}},
			// The same, over 0.4 of the opaque fill.
			{"the overlap", width/2 - overlap, height/2 - overlap, color.NRGBA{R: 173, G: 153, B: 60, A: 255}},
			{"the bottom left", width / 4, 3 * height / 4, color.NRGBA{A: 255}},
			{"the bottom right", 3 * width / 4, 3 * height / 4, color.NRGBA{A: 255}},
		} {
			if got := picture.NRGBAAt(probe.x, probe.y); got != probe.want {
				t.Errorf("%s, at (%d, %d) of %d×%d, is %v, want %v", probe.name, probe.x, probe.y, width, height, got, probe.want)
			}
		}
		return
	}
	t.Fatal("no capture of the window arrived within 5s")
}
