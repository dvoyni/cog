package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The debug shapes' geometry, judged on the vertices and indices each builder
// appends: size, centre, orientation and winding, and nothing for a shape
// with nothing to draw.

// geometryOf builds one shape's geometry the way its bake does.
func geometryOf[T debugShape](kind debugKind[T], shape T) ([]model.Vertex, []uint32) {
	return kind.geometry(nil, nil, shape)
}

// outward reports whether every triangle winds counter-clockwise seen from
// outside a convex solid about centre, which is what the bundled back-face
// cull keeps.
func outward(t *testing.T, vertices []model.Vertex, indices []uint32, centre m.Vec3) {
	t.Helper()
	for i := 0; i < len(indices); i += 3 {
		a, b, c := vertices[indices[i]].Position, vertices[indices[i+1]].Position, vertices[indices[i+2]].Position
		facing := b.Sub(a).Cross(c.Sub(a))
		middle := a.Add(b).Add(c).DivS(3)
		if facing.Dot(middle.Sub(centre)) <= 0 {
			t.Fatalf("triangle %d (%v %v %v) winds inward", i/3, a, b, c)
		}
	}
}

// bounds is the axis-aligned box the positions span.
func bounds(vertices []model.Vertex) (lo, hi m.Vec3) {
	lo, hi = vertices[0].Position, vertices[0].Position
	for _, v := range vertices[1:] {
		lo, hi = lo.Min(v.Position), hi.Max(v.Position)
	}
	return lo, hi
}

func TestADebugBoxIsItsSizeCentredAndWoundOutward(t *testing.T) {
	vertices, indices := geometryOf(debugBoxKind, DebugBox{Size: m.Vec3{X: 2, Y: 4, Z: 6}})
	if len(vertices) != 24 || len(indices) != 36 {
		t.Fatalf("the box has %d vertices and %d indices, want 24 and 36", len(vertices), len(indices))
	}
	lo, hi := bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: -1, Y: -2, Z: -3}) || !nearVec3(hi, m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Errorf("the box spans %v to %v, want ±(1, 2, 3)", lo, hi)
	}
	outward(t, vertices, indices, m.Vec3{})
}

func TestADebugSphereIsItsRadiusAtTheFixedTessellation(t *testing.T) {
	vertices, indices := geometryOf(debugSphereKind, DebugSphere{Radius: 3})
	if len(vertices) != 221 || len(indices) != 352*3 {
		t.Fatalf("the sphere has %d vertices and %d triangles, want 221 and 352", len(vertices), len(indices)/3)
	}
	for _, v := range vertices {
		if d := v.Position.Length(); d < 2.999 || d > 3.001 {
			t.Fatalf("a sphere vertex stands %v from the centre, want 3", d)
		}
	}
	outward(t, vertices, indices, m.Vec3{})
}

// A plane is n·p + d = 0: its quad is centred at -d along the unit normal and
// faces along it, and a ground plane spans Size.X along X and Size.Y along Z.
func TestADebugPlaneFollowsItsEquationAndTheTangentRule(t *testing.T) {
	vertices, indices := geometryOf(debugPlaneKind, DebugPlane{
		Normal: m.Vec3{Y: 2}, D: -3, Size: m.Vec2{X: 4, Y: 6},
	})
	if len(vertices) != 4 || len(indices) != 6 {
		t.Fatalf("the plane has %d vertices and %d indices, want one quad", len(vertices), len(indices))
	}
	lo, hi := bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: -2, Y: 3, Z: -3}) || !nearVec3(hi, m.Vec3{X: 2, Y: 3, Z: 3}) {
		t.Errorf("the ground plane spans %v to %v, want (-2, 3, -3) to (2, 3, 3)", lo, hi)
	}
	// One-sided: every triangle faces along the normal.
	for i := 0; i < len(indices); i += 3 {
		a, b, c := vertices[indices[i]].Position, vertices[indices[i+1]].Position, vertices[indices[i+2]].Position
		if b.Sub(a).Cross(c.Sub(a)).Y <= 0 {
			t.Errorf("triangle %d faces away from the normal", i/3)
		}
	}

	// A wall facing +Z at z = 5 keeps Size.X along X.
	vertices, _ = geometryOf(debugPlaneKind, DebugPlane{
		Normal: m.Vec3{Z: 1}, D: -5, Size: m.Vec2{X: 4, Y: 2},
	})
	lo, hi = bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: -2, Y: -1, Z: 5}) || !nearVec3(hi, m.Vec3{X: 2, Y: 1, Z: 5}) {
		t.Errorf("the wall spans %v to %v, want (-2, -1, 5) to (2, 1, 5)", lo, hi)
	}

	// A normal along X has no +X to project, so Size.X runs along Z.
	vertices, _ = geometryOf(debugPlaneKind, DebugPlane{
		Normal: m.Vec3{X: -1}, D: 1, Size: m.Vec2{X: 4, Y: 2},
	})
	lo, hi = bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: 1, Y: -1, Z: -2}) || !nearVec3(hi, m.Vec3{X: 1, Y: 1, Z: 2}) {
		t.Errorf("the plane facing -X spans %v to %v, want (1, -1, -2) to (1, 1, 2)", lo, hi)
	}
}

func TestADebugLineRunsBetweenItsPointsAtItsWidth(t *testing.T) {
	from, to := m.Vec3{X: 1, Y: 2, Z: 3}, m.Vec3{X: 5, Y: 2, Z: 3}
	vertices, indices := geometryOf(debugLineKind, DebugLine{From: from, To: to, Width: 0.5})
	if len(vertices) != 24 {
		t.Fatalf("the line has %d vertices, want one box's 24", len(vertices))
	}
	lo, hi := bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: 1, Y: 1.75, Z: 2.75}) || !nearVec3(hi, m.Vec3{X: 5, Y: 2.25, Z: 3.25}) {
		t.Errorf("the line spans %v to %v", lo, hi)
	}
	outward(t, vertices, indices, from.Add(to).DivS(2))

	// A vertical line takes the other reference axis and still has area.
	vertices, indices = geometryOf(debugLineKind, DebugLine{To: m.Vec3{Y: 2}, Width: 0.2})
	lo, hi = bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: -0.1, Z: -0.1}) || !nearVec3(hi, m.Vec3{X: 0.1, Y: 2, Z: 0.1}) {
		t.Errorf("the vertical line spans %v to %v", lo, hi)
	}
	outward(t, vertices, indices, m.Vec3{Y: 1})
}

func TestADebugWireBoxIsTwelveEdgesClosingItsCorners(t *testing.T) {
	vertices, indices := geometryOf(debugWireBoxKind, DebugWireBox{Size: m.Vec3{X: 2, Y: 4, Z: 6}, Width: 0.2})
	if len(vertices) != 12*24 || len(indices) != 12*36 {
		t.Fatalf("the wire box has %d vertices and %d indices, want twelve boxes", len(vertices), len(indices))
	}
	lo, hi := bounds(vertices)
	if !nearVec3(lo, m.Vec3{X: -1.1, Y: -2.1, Z: -3.1}) || !nearVec3(hi, m.Vec3{X: 1.1, Y: 2.1, Z: 3.1}) {
		t.Errorf("the wire box spans %v to %v, want ±(1.1, 2.1, 3.1)", lo, hi)
	}
	for edge := range 12 {
		box := vertices[edge*24 : edge*24+24]
		elo, ehi := bounds(box)
		outward(t, box, indices[:36], elo.Add(ehi).DivS(2))
	}
}

func TestADegenerateShapeHasNoGeometry(t *testing.T) {
	cases := map[string]int{}
	count := func(name string, vertices []model.Vertex, indices []uint32) {
		cases[name] = len(vertices) + len(indices)
	}
	count("flat box", nil, nil)
	v, i := geometryOf(debugBoxKind, DebugBox{Size: m.Vec3{X: 1, Y: 0, Z: 1}})
	count("flat box", v, i)
	v, i = geometryOf(debugSphereKind, DebugSphere{Radius: 0})
	count("zero sphere", v, i)
	v, i = geometryOf(debugPlaneKind, DebugPlane{Size: m.Vec2{X: 1, Y: 1}})
	count("plane with no normal", v, i)
	v, i = geometryOf(debugPlaneKind, DebugPlane{Normal: m.Vec3{Y: 1}, Size: m.Vec2{X: 1}})
	count("plane with no depth", v, i)
	v, i = geometryOf(debugLineKind, DebugLine{From: m.Vec3{X: 1}, To: m.Vec3{X: 1}, Width: 1})
	count("point line", v, i)
	v, i = geometryOf(debugLineKind, DebugLine{To: m.Vec3{X: 1}})
	count("line of no width", v, i)
	v, i = geometryOf(debugWireBoxKind, DebugWireBox{Width: 1})
	count("wire box of no size", v, i)
	v, i = geometryOf(debugWireBoxKind, DebugWireBox{Size: m.Vec3{X: 1, Y: -1, Z: 1}, Width: 1})
	count("inside-out wire box", v, i)
	for name, n := range cases {
		if n != 0 {
			t.Errorf("a %s built geometry", name)
		}
	}
}
