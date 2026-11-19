package canvas_test

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

func TestATranslucentStrokeShowsOneColourAlongItsSidesAndCornersAndNothingInside(t *testing.T) {
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: m.Rect{X: -100, Y: -80, Width: 200, Height: 160}, Width: 10, Tint: translucentOrange})
	})

	picture.expect(t, []probe{
		{"the top-left corner", m.Vec2{X: -97, Y: -77}, translucentOverBlack},
		{"the top-right corner", m.Vec2{X: 97, Y: -77}, translucentOverBlack},
		{"the bottom-left corner", m.Vec2{X: -97, Y: 77}, translucentOverBlack},
		{"the bottom-right corner", m.Vec2{X: 97, Y: 77}, translucentOverBlack},
		{"the top side", m.Vec2{Y: -77}, translucentOverBlack},
		{"the bottom side", m.Vec2{Y: 77}, translucentOverBlack},
		{"the left side", m.Vec2{X: -97}, translucentOverBlack},
		{"the right side", m.Vec2{X: 97}, translucentOverBlack},
		{"the middle", m.Vec2{}, opaqueBlack},
		{"just inside the inner edge", m.Vec2{X: -88, Y: -68}, opaqueBlack},
		{"just outside the rectangle", m.Vec2{X: -103, Y: -83}, opaqueBlack},
	})
}

func TestAHairlineIsOneTargetPixelWideAtAnyScale(t *testing.T) {
	// Each region draws a hairline stroke of a rectangle 120×90 world units
	// from its origin, a horizontal hairline 100.5 units below the origin,
	// and a vertical one 130.5 units right of it, scaled by the shape's
	// transform: the lines centred on a pixel's middle, each side of the
	// stroke inside a pixel's edge.
	regions := []struct {
		name   string
		origin m.Vec2
		scale  float32
	}{
		{"unscaled", m.Vec2{X: -150, Y: -110}, 1},
		{"scaled by 3", m.Vec2{X: 10, Y: -110}, 3},
	}
	picture := captureDrawing(t, func(list canvas.DrawList) {
		for _, region := range regions {
			transform := m.TRS2x3(region.origin, 0, m.Vec2{X: region.scale, Y: region.scale})
			unit := 1 / region.scale
			list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: m.Rect{Width: 120 * unit, Height: 90 * unit}, Transform: transform, Tint: m.White})
			list.DrawShape(canvas.Shape{Kind: canvas.Line, From: m.Vec2{Y: 100.5 * unit}, To: m.Vec2{X: 120 * unit, Y: 100.5 * unit}, Transform: transform, Tint: m.White})
			list.DrawShape(canvas.Shape{Kind: canvas.Line, From: m.Vec2{X: 130.5 * unit}, To: m.Vec2{X: 130.5 * unit, Y: 90 * unit}, Transform: transform, Tint: m.White})
		}
	})

	for _, region := range regions {
		at := func(x, y float32) m.Vec2 { return region.origin.Add(m.Vec2{X: x, Y: y}) }
		for _, crossing := range []struct {
			name     string
			from, to m.Vec2
		}{
			{"the stroke's top side", at(60, -5), at(60, 5)},
			{"the stroke's bottom side", at(60, 85), at(60, 95)},
			{"the stroke's left side", at(-5, 45), at(5, 45)},
			{"the stroke's right side", at(115, 45), at(125, 45)},
			{"the horizontal line", at(60, 96), at(60, 105)},
			{"the vertical line", at(126, 45), at(135, 45)},
		} {
			if lit := picture.countLit(crossing.from, crossing.to); lit != 1 {
				t.Errorf("%s, %s, is %d pixels wide from %v to %v, want 1", region.name, crossing.name, lit, crossing.from, crossing.to)
			}
		}
		if got, want := picture.at(at(60, 100.5)), opaqueWhite; got != want {
			t.Errorf("%s, the horizontal line's pixel is %v, want its tint, %v", region.name, got, want)
		}
	}
}

func TestARoundedFillAndStrokeLeaveTheirCornersEmpty(t *testing.T) {
	// Both are 100 units square, their corners' curves of radius 25 about
	// points 25 in from each side, the stroke 6 wide and translucent, so
	// its sides overlapping would show darker.
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -140, Y: -60, Width: 100, Height: 100}, Radius: 25, Tint: green})
		list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: m.Rect{X: 20, Y: -60, Width: 100, Height: 100}, Width: 6, Radius: 25, Tint: translucentOrange})
	})

	picture.expect(t, []probe{
		{"the fill's top-left corner", m.Vec2{X: -138, Y: -58}, opaqueBlack},
		{"the fill's top-right corner", m.Vec2{X: -42, Y: -58}, opaqueBlack},
		{"the fill's bottom-left corner", m.Vec2{X: -138, Y: 38}, opaqueBlack},
		{"the fill's bottom-right corner", m.Vec2{X: -42, Y: 38}, opaqueBlack},
		{"the fill just outside its curve", m.Vec2{X: -135, Y: -55}, opaqueBlack},
		{"the fill just inside its curve", m.Vec2{X: -130, Y: -50}, greenShown},
		{"the fill's top side", m.Vec2{X: -90, Y: -58}, greenShown},
		{"the fill's left side", m.Vec2{X: -138, Y: -10}, greenShown},
		{"the fill's middle", m.Vec2{X: -90, Y: -10}, greenShown},
		{"the stroke's top-left corner", m.Vec2{X: 22, Y: -58}, opaqueBlack},
		{"the stroke's bottom-right corner", m.Vec2{X: 118, Y: 38}, opaqueBlack},
		{"the stroke on its curve", m.Vec2{X: 30, Y: -50}, translucentOverBlack},
		{"the stroke inside its curve", m.Vec2{X: 35, Y: -45}, opaqueBlack},
		{"the stroke's top side", m.Vec2{X: 70, Y: -58}, translucentOverBlack},
		{"the stroke's left side", m.Vec2{X: 22, Y: -10}, translucentOverBlack},
		{"the stroke's inside, by its top side", m.Vec2{X: 70, Y: -50}, opaqueBlack},
		{"the stroke's inside, by its left side", m.Vec2{X: 28, Y: -10}, opaqueBlack},
	})
}

func TestAFillAStrokeAndALineOfOneMaterialAreOneBatchOfEachOfTheirInstances(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 4, Height: 4}, Tint: m.White})
		list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: m.Rect{Width: 8, Height: 8}, Width: 1, Radius: 2, Tint: m.White})
		list.DrawShape(canvas.Shape{Kind: canvas.Line, To: m.Vec2{X: 10, Y: 10}, Tint: m.White})
	})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 1 || len(batches[0].Entries) != 3 || batches[0].InstanceCount != 6 {
		t.Fatalf("the shapes drew as %+v, want one batch of their three entries and six instances: a stroke's four sides and the others' one", batches)
	}
	draws := findCanvasLists(frame.Render)[0].Passes[0].Draws
	if len(draws) != 1 || draws[0].InstanceCount != 6 || draws[0].FirstInstance != 0 {
		t.Errorf("canvas's pass drew %+v, want one draw of 6 instances from instance 0", draws)
	}
}
