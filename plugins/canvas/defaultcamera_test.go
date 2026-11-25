package canvas_test

import (
	"reflect"
	"slices"
	"testing"

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
	passes := dropMipPasses(lists[1]).Passes
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
	// The default camera centres the world's origin in the window, a world
	// unit to a point and y down: the opaque fill covers the centre, the
	// translucent one, recorded before it but drawn after it, a layer up,
	// most of the top-left quarter and the opaque fill's top-left corner,
	// and nothing the bottom half.
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -150, Y: -110, Width: 145, Height: 105}, Tint: translucentOrange, Layer: 1})
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -20, Y: -20, Width: 40, Height: 40}, Tint: green})
	})

	picture.expect(t, []probe{
		{"the opaque fill's middle", m.Vec2{}, greenShown},
		{"the translucent fill's middle", m.Vec2{X: -80, Y: -60}, translucentOverBlack},
		// 0.6 of the encoded (255, 85, 0) over 0.4 of the opaque fill.
		{"the overlap", m.Vec2{X: -12, Y: -12}, translucentOverGreen},
		{"the bottom left", m.Vec2{X: -80, Y: 60}, opaqueBlack},
		{"the bottom right", m.Vec2{X: 80, Y: 60}, opaqueBlack},
	})
}
