package types

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// massShape builds the port's Shape for one row of the mass corpus, the way an
// app would build it.
func massShape(t *testing.T, row cpMassCase) (Shape, Polygon) {
	t.Helper()
	verts := make([]m.Vec2d, len(row.verts))
	for i, v := range row.verts {
		verts[i] = m.Vec2d{X: v[0], Y: v[1]}
	}
	switch row.kind {
	case "circle":
		return NewCircleShape(row.radius, verts[0]), Polygon{}
	case "segment":
		return NewSegmentShape(verts[0], verts[1], row.radius), Polygon{}
	case "box":
		return NewBoxShapeFor(NewBB(verts[0].X, verts[0].Y, verts[1].X, verts[1].Y), row.radius), Polygon{}
	case "poly":
		shape, polygon, err := NewPolygonShape(verts, row.radius)
		if err != nil {
			t.Fatalf("%s: NewPolygonShape refused the outline: %v", row.name, err)
		}
		return shape, polygon
	}
	t.Fatalf("%s: unknown kind %q", row.name, row.kind)
	return Shape{}, Polygon{}
}

// localPoints is every point a Shape carries in its own frame: a circle's
// offset, a segment's two endpoints, a polygon's vertices. How far the
// constructor moved each of them is the centroid it found.
func localPoints(shape Shape, polygon Polygon) []m.Vec2d {
	switch shape.Kind {
	case ShapeCircle:
		return []m.Vec2d{shape.Offset()}
	case ShapeSegment:
		return []m.Vec2d{shape.A(), shape.B()}
	}
	return PolygonVerts(nil, shape, polygon)
}

// For every kind, the constructor's mass, Moment of inertia and centre of
// gravity are cp's, as AccumulateMassFromShapes computes them for a body of
// that one Shape at that density. The centre of gravity is read off how far the
// constructor moved each of the Shape's own points, which has to be the same
// vector for every one of them.
func TestNewDynamicForShapeAgreesWithAccumulateMassFromShapes(t *testing.T) {
	for _, row := range cpMassCases {
		shape, polygon := massShape(t, row)

		body, recentred, recentredPolygon, err := NewDynamicForShape(shape, polygon, row.density, 0, 0)
		if err != nil {
			t.Fatalf("%s: refused: %v", row.name, err)
		}
		if got := body.Mass(); math.Abs(got-row.mass) > cpTolerance {
			t.Errorf("%s: mass %v, cp's %v", row.name, got, row.mass)
		}
		if got := body.Moment(); math.Abs(got-row.moment) > cpTolerance {
			t.Errorf("%s: moment %v, cp's %v", row.name, got, row.moment)
		}

		before, after := localPoints(shape, polygon), localPoints(recentred, recentredPolygon)
		if len(before) != len(after) {
			t.Fatalf("%s: %d points became %d", row.name, len(before), len(after))
		}
		cog := m.Vec2d{X: row.cog[0], Y: row.cog[1]}
		for i := range before {
			moved := before[i].Sub(after[i])
			if math.Abs(moved.X-cog.X) > cpTolerance || math.Abs(moved.Y-cog.Y) > cpTolerance {
				t.Errorf("%s: point %d moved by %v, cp's centre of gravity is %v", row.name, i, moved, cog)
			}
		}
	}
}

// The recentred Shape's own centre of gravity is its local origin, which is
// what Position means.
func TestNewDynamicForShapePutsTheCentroidAtTheOrigin(t *testing.T) {
	for _, row := range cpMassCases {
		shape, polygon := massShape(t, row)
		_, recentred, recentredPolygon, err := NewDynamicForShape(shape, polygon, row.density, 0, 0)
		if err != nil {
			t.Fatalf("%s: refused: %v", row.name, err)
		}

		var centroid m.Vec2d
		switch recentred.Kind {
		case ShapeCircle:
			if recentred.Offset() != (m.Vec2d{}) {
				t.Errorf("%s: a recentred circle's offset is %v, want exactly zero", row.name, recentred.Offset())
			}
			continue
		case ShapeSegment:
			centroid = recentred.A().Lerp(recentred.B(), 0.5)
		default:
			var ok bool
			centroid, ok = CentroidForPoly(PolygonVerts(nil, recentred, recentredPolygon))
			if !ok {
				t.Fatalf("%s: the recentred outline is degenerate", row.name)
			}
		}
		if math.Abs(centroid.X) > cpTolerance || math.Abs(centroid.Y) > cpTolerance {
			t.Errorf("%s: the recentred Shape's centroid is %v, want the origin", row.name, centroid)
		}
		if recentred.Kind != shape.Kind || recentred.Radius != shape.Radius {
			t.Errorf("%s: kind %v radius %v became kind %v radius %v",
				row.name, shape.Kind, shape.Radius, recentred.Kind, recentred.Radius)
		}
	}
}

// Everything but the geometry comes through as it was given: the material, the
// two collision fields, Sensor, and a segment's neighbour tangents, which are
// relative to the endpoints and so do not move with them.
func TestNewDynamicForShapeCarriesEverythingButTheGeometry(t *testing.T) {
	previous, a, b, next := m.Vec2d{X: -2, Y: 1}, m.Vec2d{X: 1, Y: 1}, m.Vec2d{X: 3, Y: 2}, m.Vec2d{X: 5, Y: 0}
	shape := NewSegmentShapeWithNeighbours(previous, a, b, next, 0.25)
	shape.Friction = 0.7
	shape.Restitution = 0.3
	shape.CollisionBits = 0b0110
	shape.CollidesWith = 0b1001
	shape.Sensor = true

	_, got, _, err := NewDynamicForShape(shape, Polygon{}, 2, 0, 0)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got.Friction != shape.Friction || got.Restitution != shape.Restitution ||
		got.CollisionBits != shape.CollisionBits || got.CollidesWith != shape.CollidesWith ||
		got.Sensor != shape.Sensor {
		t.Errorf("the recentred Shape is %+v, want the material, filter bits and Sensor of %+v", got, shape)
	}
	if got.verts[2] != shape.verts[2] || got.verts[3] != shape.verts[3] {
		t.Errorf("the neighbour tangents moved from %v, %v to %v, %v",
			shape.verts[2], shape.verts[3], got.verts[2], got.verts[3])
	}
}

// The Polygon the caller passed shares nothing with the one that comes back:
// its vertices are where they were, and the recentred ones are elsewhere.
func TestNewDynamicForShapeNeverWritesTheCallersPolygon(t *testing.T) {
	hexagon := []m.Vec2d{{X: 3, Y: 1}, {X: 2.5, Y: 1.866}, {X: 1.5, Y: 1.866}, {X: 1, Y: 1}, {X: 1.5, Y: 0.134}, {X: 2.5, Y: 0.134}}
	shape, polygon, err := NewPolygonShape(hexagon, 0)
	if err != nil {
		t.Fatalf("NewPolygonShape: %v", err)
	}
	given := PolygonVerts(nil, shape, polygon)

	_, _, recentred, err := NewDynamicForShape(shape, polygon, 1, 0, 0)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}

	if after := PolygonVerts(nil, shape, polygon); !equalVerts(after, given) {
		t.Fatalf("the caller's Polygon was written: %v, was %v", after, given)
	}
	if moved := PolygonVerts(nil, shape, recentred); equalVerts(moved, given) {
		t.Fatalf("the returned Polygon is the caller's own, unmoved: %v", moved)
	}
}

// An inline kind takes no Polygon, and the one it is given comes back as the
// zero Polygon rather than as something the app then spawns by mistake.
func TestNewDynamicForShapeReturnsTheZeroPolygonForAnInlineKind(t *testing.T) {
	_, stray, err := NewPolygonShape([]m.Vec2d{{}, {X: 2}, {X: 2, Y: 1}, {X: 1, Y: 2}, {Y: 1}}, 0)
	if err != nil {
		t.Fatalf("NewPolygonShape: %v", err)
	}
	for name, shape := range map[string]Shape{
		"circle":  NewCircleShape(1, m.Vec2d{X: 1}),
		"segment": NewSegmentShape(m.Vec2d{}, m.Vec2d{X: 1}, 0.2),
		"box":     NewBoxShapeFor(NewBB(0, 0, 2, 1), 0),
	} {
		_, _, polygon, err := NewDynamicForShape(shape, stray, 1, 0, 0)
		if err != nil {
			t.Fatalf("%s: refused: %v", name, err)
		}
		if polygon.Verts.Len() != 0 {
			t.Errorf("%s: came back with a Polygon of %d vertices, want the zero Polygon", name, polygon.Verts.Len())
		}
	}
}

// Each refusal names its reason and hands back the zero Dynamic, with the Shape
// and the Polygon exactly as they were given, so a caller that ignores the
// error spawns a Body that visibly does nothing where it asked for it.
func TestNewDynamicForShapeRefusesWhatHasNoMass(t *testing.T) {
	hexagon := []m.Vec2d{{X: 3, Y: 1}, {X: 2.5, Y: 1.866}, {X: 1.5, Y: 1.866}, {X: 1, Y: 1}, {X: 1.5, Y: 0.134}, {X: 2.5, Y: 0.134}}
	poly, polygon, err := NewPolygonShape(hexagon, 0)
	if err != nil {
		t.Fatalf("NewPolygonShape: %v", err)
	}
	circle := NewCircleShape(0.5, m.Vec2d{X: 1})

	for _, c := range []struct {
		name                    string
		shape                   Shape
		polygon                 Polygon
		density                 float64
		damping, angularDamping float64
		want                    error
	}{
		{"a point", NewCircleShape(0, m.Vec2d{X: 1}), Polygon{}, 1, 0, 0, ErrNoArea},
		{"a bare segment", NewSegmentShape(m.Vec2d{}, m.Vec2d{X: 2}, 0), Polygon{}, 1, 0, 0, ErrNoArea},
		{"a Poly with no Polygon", poly, Polygon{}, 1, 0, 0, ErrNoArea},
		{"a zero density", circle, Polygon{}, 0, 0, 0, ErrBadDensity{Density: 0}},
		{"a negative density", poly, polygon, -1, 0, 0, ErrBadDensity{Density: -1}},
		{"a NaN density", circle, Polygon{}, math.NaN(), 0, 0, ErrBadDensity{}},
		{"an infinite density", circle, Polygon{}, math.Inf(1), 0, 0, ErrBadDensity{Density: math.Inf(1)}},
		{"a mass that overflows", poly, polygon, math.MaxFloat64, 0, 0, ErrBadMass{Mass: math.Inf(1)}},
		{"a negative damping", poly, polygon, 1, -1, 0, ErrBadDamping{Rate: -1}},
	} {
		body, shape, gotPolygon, err := NewDynamicForShape(c.shape, c.polygon, c.density, c.damping, c.angularDamping)

		switch want := c.want.(type) {
		case ErrBadDensity:
			var got ErrBadDensity
			if !errors.As(err, &got) {
				t.Errorf("%s: err = %v, want ErrBadDensity", c.name, err)
			} else if !math.IsNaN(c.density) && got.Density != want.Density {
				t.Errorf("%s: ErrBadDensity carries %v, want %v", c.name, got.Density, want.Density)
			}
		default:
			if !errors.Is(err, c.want) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			}
		}
		if body != (Dynamic{}) {
			t.Errorf("%s: came back with %+v, want the zero Dynamic", c.name, body)
		}
		if shape != c.shape {
			t.Errorf("%s: the Shape came back as %+v, want it as given", c.name, shape)
		}
		if !equalVerts(PolygonVerts(nil, c.shape, gotPolygon), PolygonVerts(nil, c.shape, c.polygon)) {
			t.Errorf("%s: the Polygon came back changed", c.name)
		}
	}
}

// The inline kinds need nothing from the heap. A Poly allocates its new vertex
// List, which is the documented cost of never writing the caller's.
func TestNewDynamicForShapeAllocatesOnlyAPolysNewVertices(t *testing.T) {
	box := NewBoxShapeFor(NewBB(0, 0, 2, 1), 0.1)
	for name, shape := range map[string]Shape{
		"circle":  NewCircleShape(1, m.Vec2d{X: 1}),
		"segment": NewSegmentShape(m.Vec2d{}, m.Vec2d{X: 1}, 0.2),
		"box":     box,
	} {
		allocations := testing.AllocsPerRun(100, func() {
			var err error
			_, shapeSink, _, err = NewDynamicForShape(shape, Polygon{}, 1, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
		})
		if allocations != 0 {
			t.Errorf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}

func equalVerts(a, b []m.Vec2d) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
