package canvas

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestALinesInstanceMapsTheUnitSquareOntoABandItsWidthWideCentredOnItsSegment(t *testing.T) {
	line := Shape{Kind: Line, From: m.Vec2{X: 10, Y: 20}, To: m.Vec2{X: 10, Y: 60}, Width: 4, Radius: 3, Transform: m.Translation2x3(100, 0)}

	instances := appendShapeInstances(nil, &line)

	if len(instances) != 1 {
		t.Fatalf("a line drew %d instances, want 1", len(instances))
	}
	instance := instances[0]
	for _, corner := range []struct{ unit, want m.Vec2 }{
		{m.Vec2{}, m.Vec2{X: 112, Y: 20}},
		{m.Vec2{X: 1}, m.Vec2{X: 112, Y: 60}},
		{m.Vec2{Y: 1}, m.Vec2{X: 108, Y: 20}},
		{m.Vec2{X: 1, Y: 1}, m.Vec2{X: 108, Y: 60}},
	} {
		if got := instance.transform.TransformPoint(corner.unit); got != corner.want {
			t.Errorf("the line's square maps %v to %v, want %v: along the segment, 2 to either side of it, then moved by its transform", corner.unit, got, corner.want)
		}
	}
	if instance.part != shapePartLine || instance.size != (m.Vec2{X: 40, Y: 4}) || instance.width != 4 || instance.radius != 0 {
		t.Errorf("the line's instance is part %d, size %v, width %v, radius %v, want the line part, 40×4, width 4 and no radius, which a line ignores", instance.part, instance.size, instance.width, instance.radius)
	}
}

func TestAHairlinesSquareIsOneUnitAcrossForTheVertexStageToNarrow(t *testing.T) {
	instances := appendShapeInstances(nil, &Shape{Kind: Line, From: m.Vec2{X: 1, Y: 2}, To: m.Vec2{X: 4, Y: 6}, Width: -2})

	instance := instances[0]
	if instance.width != 0 || instance.size != (m.Vec2{X: 5, Y: 1}) {
		t.Errorf("a hairline's instance is width %v and size %v, want 0 and 5×1", instance.width, instance.size)
	}
	if got, want := instance.transform.TransformPoint(m.Vec2{Y: 0.5}), (m.Vec2{X: 1, Y: 2}); got != want {
		t.Errorf("the hairline's square's middle starts at %v, want its From, %v", got, want)
	}
}

func TestAStrokeDrawsItsFourSidesAndAFillIgnoresItsWidth(t *testing.T) {
	rect := m.Rect{X: 5, Y: 6, Width: -20, Height: 10}
	stroke := Shape{Kind: Stroke, Rect: rect, Width: 2, Radius: 3, Tint: m.Color{R: 1, A: 0.5}}

	instances := appendShapeInstances(nil, &stroke)

	var parts []shapePart
	for _, instance := range instances {
		parts = append(parts, instance.part)
		if instance.width != 2 || instance.radius != 3 || instance.size != (m.Vec2{X: 20, Y: 10}) || instance.tint != [4]float32{0.5, 0, 0, 0.5} {
			t.Errorf("a stroke's side is width %v, radius %v, size %v and tint %v, want 2, 3, 20×10 and its tint premultiplied", instance.width, instance.radius, instance.size, instance.tint)
		}
	}
	if want := []shapePart{shapePartStrokeTop, shapePartStrokeBottom, shapePartStrokeLeft, shapePartStrokeRight}; !slices.Equal(parts, want) {
		t.Errorf("a stroke drew the parts %v, want its four sides, %v", parts, want)
	}

	fill := appendShapeInstances(nil, &Shape{Kind: Fill, Rect: rect, Width: 2, Radius: -3})

	if len(fill) != 1 || fill[0].part != shapePartWhole || fill[0].width != 0 || fill[0].radius != 0 {
		t.Errorf("a fill drew %+v, want one instance of the whole square, its width ignored and its negative radius none", fill)
	}
}
