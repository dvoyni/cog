package types

import (
	"errors"

	"github.com/dvoyni/cog/libs/m"
)

// Polygon is the vertices of a Shape too large to carry them inline: the second
// Component a Shape of kind ShapePoly needs, on the same Entity beside it.
//
// Index probes it with ecs.Get and copies it into the world cache once a tick;
// nothing on the hot path touches it again. It exists because a Shape is 104
// bytes with four vertex slots, and a fifth vertex has nowhere inline to go —
// so there is no vertex cap at all, which matters because cp has none either.
//
// Its vertices are local to the Body's Position, wound as NewPolygonShape wound
// them, and they are the constructor's output rather than the app's input: a
// Polygon written by hand is not hulled and is not checked.
type Polygon struct {
	// Verts are the convex outline's vertices. m.List is the storable answer
	// to a variable-length run in a Component: it yields copies and hands out no
	// slice, which is why Index copies rather than pointing at it.
	Verts m.List[m.Vec2d]
}

// The three ways a Shape constructor refuses an outline. They are sentinels
// rather than structs carrying the outline, so the failure path allocates
// nothing and a caller compares with errors.Is.
//
// cp refuses none of them: cpPolyValidate is gone from this version, the raw
// constructors accept anything, and Go's asserts compile out by default, so in
// cp a concave outline collides as its hull for ever with nothing said.
var (
	// ErrTooFewVertices reports an outline of fewer than three vertices, which
	// encloses no region.
	ErrTooFewVertices = errors.New("ecsphysics2d: a polygon needs at least three vertices")

	// ErrDegenerateOutline reports an outline with no area — coincident or
	// collinear vertices — which is what makes cp's CentroidForPoly divide by
	// zero and hand back a NaN.
	ErrDegenerateOutline = errors.New("ecsphysics2d: the polygon outline has no area")

	// ErrConcaveOutline reports an outline the hull changed, which is the
	// concave case and also the redundant one: a vertex inside the hull, or on
	// an edge of it, is a vertex the hull drops. A concave Polygon does not
	// silently become its hull.
	ErrConcaveOutline = errors.New("ecsphysics2d: the polygon outline is not convex")
)

// maxInlineVerts is how many vertices a Shape carries in its own four slots, so
// that a triangle and a box need no second Component. It is a constant and not
// a design: raising it is a source change for apps, because a Shape spawned
// with a Polygon Component beside it would become an inline kind.
const maxInlineVerts = 4

// NewPolygonShape is the convex polygon of those vertices, local to the Body's
// Position and rounded by that radius. It is the one way in, and it always
// hulls: cp's ConvexHull, whose output order is the clockwise winding
// AreaForPoly treats as positive, so a counter-clockwise outline is rewound
// rather than given a negative mass.
//
// Three or four vertices come back as ShapeTri or ShapeQuad with the vertices
// in the Shape's own slots and the zero Polygon beside them; more come back as
// ShapePoly with the vertices in the Polygon, which the app spawns on the same
// Entity. Spawning the zero Polygon beside an inline kind is harmless, so a
// caller may always spawn both.
//
// A refused outline comes back as a point — a circle of radius 0 at the local
// origin, which collides with almost nothing and cannot be mistaken for what
// was asked for — with the zero Polygon and one of the three sentinels:
// ErrTooFewVertices, ErrDegenerateOutline, or ErrConcaveOutline.
func NewPolygonShape(verts []m.Vec2d, radius float64) (Shape, Polygon, error) {
	if len(verts) < 3 {
		return NewCircleShape(0, m.Vec2d{}), Polygon{}, ErrTooFewVertices
	}

	hull := make([]m.Vec2d, len(verts))
	copy(hull, verts)
	count := convexHull(hull)
	hull = hull[:count]

	// The three tests run on the hull rather than on what the app wrote, and
	// they have to: an outline given out of order is a self-crossing loop of
	// zero signed area until it is hulled, so testing the input for area would
	// refuse exactly the input the hull is there to sort out.
	switch {
	case count < 3:
		// Coincident or collinear vertices, which collapse to a point or an
		// edge and enclose nothing.
		return NewCircleShape(0, m.Vec2d{}), Polygon{}, ErrDegenerateOutline
	case count != len(verts):
		return NewCircleShape(0, m.Vec2d{}), Polygon{}, ErrConcaveOutline
	}
	// The divide cp's CentroidForPoly does unguarded, asked before anything
	// depends on the answer.
	if _, ok := CentroidForPoly(hull); !ok {
		return NewCircleShape(0, m.Vec2d{}), Polygon{}, ErrDegenerateOutline
	}

	return newHulledShape(hull, radius)
}

// NewBoxShape is a box of that width and height centred on the Body's Position,
// rounded by that radius. It is cp's NewBox, which keeps its four vertices
// directly rather than hulling them: a box built from its own extents is convex
// and wound by construction, so there is nothing for a hull to find out.
func NewBoxShape(width, height, radius float64) Shape {
	halfWidth, halfHeight := width/2, height/2
	return NewBoxShapeFor(NewBB(-halfWidth, -halfHeight, halfWidth, halfHeight), radius)
}

// NewBoxShapeFor is cp's NewBox2: the box with those four edges, local to the
// Body's Position, rounded by that radius. An off-centre box is what a wall
// drawn around something other than its own middle is.
func NewBoxShapeFor(box BB, radius float64) Shape {
	shape := Shape{
		Radius:        radius,
		CollisionBits: CollisionBitsAll,
		CollidesWith:  CollisionBitsAll,
		Kind:          ShapeQuad,
	}
	// cp's vertex order, which is the winding AreaForPoly reads as positive.
	shape.verts[0] = m.Vec2d{X: box.R, Y: box.B}
	shape.verts[1] = m.Vec2d{X: box.R, Y: box.T}
	shape.verts[2] = m.Vec2d{X: box.L, Y: box.T}
	shape.verts[3] = m.Vec2d{X: box.L, Y: box.B}
	shape.faceDistance = faceDistanceOf(shape.verts[:])
	return shape
}

// newHulledShape is the Shape and the Polygon a hulled outline becomes: the
// kind the vertex count names, and the vertices wherever that kind keeps them.
func newHulledShape(hull []m.Vec2d, radius float64) (Shape, Polygon, error) {
	shape := Shape{
		Radius:        radius,
		CollisionBits: CollisionBitsAll,
		CollidesWith:  CollisionBitsAll,
	}
	switch len(hull) {
	case 3:
		shape.Kind = ShapeTri
	case maxInlineVerts:
		shape.Kind = ShapeQuad
	default:
		shape.Kind = ShapePoly
		shape.faceDistance = faceDistanceOf(hull)
		return shape, Polygon{Verts: m.ListOf(hull)}, nil
	}
	copy(shape.verts[:], hull)
	shape.faceDistance = faceDistanceOf(hull)
	return shape, Polygon{}, nil
}

// PolygonVerts appends a Shape's local vertices to dst and returns it, which is
// how a caller builds the run the queries take: the Polygon Component's for
// ShapePoly and the Shape's own slots for ShapeTri and ShapeQuad, and nothing
// at all for the two kinds that are not polygons.
//
// It exists because m.List hands out no slice, deliberately, and a query
// primitive cannot take a Component of the ECS's without the types package
// naming one in a signature the root forwards. The idiom is
// dst = PolygonVerts(dst[:0], shape, polygon), which settles to no allocation
// once the buffer is big enough.
func PolygonVerts(dst []m.Vec2d, shape Shape, polygon Polygon) []m.Vec2d {
	switch shape.Kind {
	case ShapeTri:
		return append(dst, shape.verts[:3]...)
	case ShapeQuad:
		return append(dst, shape.verts[:maxInlineVerts]...)
	case ShapePoly:
		for i := range polygon.Verts.Len() {
			dst = append(dst, polygon.Verts.At(i))
		}
	}
	return dst
}

// polyCount is how many vertices a polygon kind has: three or four in the
// Shape's own slots, or however many the Polygon Component holds. The kind
// carries the count, so there is no count field to contradict it.
func polyCount(shape Shape, verts []m.Vec2d) int {
	switch shape.Kind {
	case ShapeTri:
		return 3
	case ShapeQuad:
		return maxInlineVerts
	case ShapePoly:
		return len(verts)
	}
	return 0
}

// polyVert is a polygon kind's i-th local vertex, from the Shape's own slots or
// from the run the Polygon Component was copied into.
func polyVert(shape *Shape, verts []m.Vec2d, i int) m.Vec2d {
	if shape.Kind == ShapePoly {
		return verts[i]
	}
	return shape.verts[i]
}

// isPolygon reports that a kind keeps vertices and derived face normals, which
// is the third of the three families the pair dispatch switches on.
func isPolygon(kind ShapeKind) bool {
	return kind == ShapeTri || kind == ShapeQuad || kind == ShapePoly
}

// polyWorld splits a polygon's run of the world cache into its world vertices
// and the face normals derived beside them, one per vertex: normal i belongs to
// the edge from vertex i−1 to vertex i, which is cp's SetVerts plane order.
func polyWorld(world []m.Vec2d) (verts, normals []m.Vec2d) {
	count := len(world) / 2
	return world[:count], world[count : 2*count]
}

// convexHull is cp's ConvexHull (QuickHull), reducing verts in place and
// returning how many of its leading entries are the hull.
//
// cp's first and tol parameters are dropped: every call site in cp passes nil
// and 0, and the port has one call site, which is the hulling constructor. The
// vertex count goes with them, a Go slice saying its own length.
//
// The output order is the winding the rest of the package reads: the leftmost
// vertex, then one side of the hull to the rightmost, then the other side back.
// That is what makes the constructor enforce the winding rather than check it.
func convexHull(verts []m.Vec2d) int {
	start, end := loopIndexes(verts)
	if start == end {
		return 1
	}

	verts[0], verts[start] = verts[start], verts[0]
	if end == 0 {
		verts[1], verts[start] = verts[start], verts[1]
	} else {
		verts[1], verts[end] = verts[end], verts[1]
	}

	a := verts[0]
	b := verts[1]

	return qHullReduce(verts[2:], a, b, a, verts[1:]) + 1
}

// loopIndexes is cp's LoopIndexes: the leftmost and rightmost vertices, ties
// broken downwards and upwards, which are the two the hull is grown between.
func loopIndexes(verts []m.Vec2d) (int, int) {
	start, end := 0, 0
	minimum, maximum := verts[0], verts[0]

	for i := 1; i < len(verts); i++ {
		v := verts[i]
		switch {
		case v.X < minimum.X || (v.X == minimum.X && v.Y < minimum.Y):
			minimum = v
			start = i
		case v.X > maximum.X || (v.X == maximum.X && v.Y > maximum.Y):
			maximum = v
			end = i
		}
	}
	return start, end
}

// qHullReduce is cp's QHullReduce, the recursive half of QuickHull, writing its
// answer into result as it goes. The recursion is kept as written.
func qHullReduce(verts []m.Vec2d, a, pivot, b m.Vec2d, result []m.Vec2d) int {
	if len(verts) == 0 {
		result[0] = pivot
		return 1
	}

	leftCount := qHullPartition(verts, a, pivot)
	var index int
	if leftCount-1 >= 0 {
		index = qHullReduce(verts[1:leftCount], a, verts[0], pivot, result)
	}

	result[index] = pivot
	index++

	rightCount := qHullPartition(verts[leftCount:], pivot, b)
	if rightCount-1 < 0 {
		return index
	}
	return index + qHullReduce(verts[leftCount+1:leftCount+rightCount], pivot, verts[leftCount], b, result[index:])
}

// qHullPartition is cp's QHullPartition: it moves the vertices outside the line
// from a to b to the front, the farthest of them first, and reports how many
// there are.
//
// cp's tol is dropped with it, every call site passing 0, which makes the test
// "strictly outside" — so a vertex exactly on the line is dropped, and the
// constructor's hull-changed test refuses the outline that held it.
func qHullPartition(verts []m.Vec2d, a, b m.Vec2d) int {
	if len(verts) == 0 {
		return 0
	}

	maximum := 0.0
	pivot := 0
	delta := b.Sub(a)

	head := 0
	for tail := len(verts) - 1; head <= tail; {
		value := verts[head].Sub(a).Cross(delta)
		if value > 0 {
			if value > maximum {
				maximum = value
				pivot = head
			}
			head++
		} else {
			verts[head], verts[tail] = verts[tail], verts[head]
			tail--
		}
	}

	if pivot != 0 {
		verts[0], verts[pivot] = verts[pivot], verts[0]
	}
	return head
}
