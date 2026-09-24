package internal

import "github.com/dvoyni/cog/libs/m"

// magicEpsilon is cp's MAGIC_EPSILON, the distance below which a direction
// derived by dividing by that distance is taken to be unreliable and a fallback
// is used instead.
const magicEpsilon = 1e-5

// ShapeKind is the one convex region a Shape is, and the kind carries the
// vertex count: Circle uses one vertex slot as its centre offset, Segment two
// for its endpoints and two more for its neighbours' tangents, Tri three and
// Quad four, and Poly keeps its vertices in a Polygon Component beside the
// Shape. A count field contradicting its kind cannot be spelled.
//
// Every kind carries the radius, so a rounded segment is a capsule and a point
// is a circle of radius 0.
type ShapeKind uint8

const (
	// ShapeCircle is a circle about verts[0], the centre offset.
	ShapeCircle ShapeKind = iota
	// ShapeSegment is the segment verts[0] to verts[1], with the neighbours'
	// tangents at verts[2] and verts[3] when the app supplied them.
	ShapeSegment
	// ShapeTri is the triangle verts[0..2].
	ShapeTri
	// ShapeQuad is the quadrilateral verts[0..3].
	ShapeQuad
	// ShapePoly is a convex polygon of more than four vertices, which are in a
	// Polygon Component on the same Entity rather than inline.
	ShapePoly
)

// CollisionBitsAll is every group, which is what a constructor sets both of a
// Shape's two fields to: cp's shapes start at SHAPE_FILTER_ALL, so a Shape
// built the normal way collides with everything.
const CollisionBitsAll uint32 = 0xFFFFFFFF

// CollisionBitsNone is no group at all. A Shape with no CollisionBits is in
// nothing and is seen by nothing; a Shape with no CollidesWith looks for
// nothing. Either way it collides with nothing, as in cp — and a Shape written
// as a bare literal is in that state.
const CollisionBitsNone uint32 = 0

// Shape is the one convex region a Body occupies, 104 bytes, whose Kind names
// how many of its four vertex slots mean anything.
//
// Ported from cp's Circle, Segment and PolyShape, which are three structs
// behind an interface; one value type replaces them because a Component is one
// per Entity and a kind is not a structural change. Every field is exported,
// as every Component's is, so a Shape serialises whole; but Verts carries an
// invariant with Kind, and is written only by the constructors - never set it
// directly. Everything else is a plain field, so a Shape is data the app
// writes.
//
// Local normals are not stored. They are derived when the world cache is built,
// which is once at insert for a static Entity, so the sqrt falls only on moving
// geometry.
//
// Friction is cp's u and Restitution is cp's e, and both are plain fields
// rather than constructor arguments, because the range is cp's and cp validates
// neither. Detect combines a pair as the plain products u = ua·ub and
// e = ea·eb, so one slippery Shape is enough to make a pair slide and one dead
// Shape absorbs the bounce.
//
//   - Friction runs from 0, frictionless, upwards; 1 is about wood on wood.
//     Solve clamps the friction impulse to ±u·jn, so at 0 the tangent is left
//     alone and sliding along a wall is exactly v ← v − (v·n)·n. Above 1 is
//     legal and means a pair gripping harder than it presses.
//   - Restitution is how much of its approach speed a Body keeps when it
//     bounces: 0 stops it dead against the surface and 1 sends it away as fast
//     as it arrived. Above 1 is legal and hands back more than arrived, which
//     is cp's own behaviour and is left to the app.
//
// Both default to 0, as cp's do, so a Shape nobody has written a material onto
// behaves exactly as it did before either was spent. A Contact's Friction is
// not a Body's Damping and not a Spring's Absorption; the three are different
// things.
type Shape struct {
	// Verts are the kind's vertices, local to the Body's Position. Do not write
	// them directly: NewCircleShape, NewSegmentShape, NewPolygonShape and
	// NewBoxShape fill them for their Kind.
	Verts                       [4]m.Vec2d
	Radius                      float64
	Friction                    float64
	Restitution                 float64
	CollisionBits, CollidesWith uint32
	Kind                        ShapeKind
	Sensor                      bool
	_                           [6]byte
}

// NewCircleShape is a circle of that radius about an offset from the Body's
// Position, which is its centre of gravity. A radius of 0 is a point.
func NewCircleShape(radius float64, offset m.Vec2d) Shape {
	shape := Shape{
		Radius:        radius,
		CollisionBits: CollisionBitsAll,
		CollidesWith:  CollisionBitsAll,
		Kind:          ShapeCircle,
	}
	shape.Verts[0] = offset
	return shape
}

// NewSegmentShape is the segment from a to b, both local to the Body's
// Position, fattened by that radius. Fattened it is a capsule; at radius 0 it
// is a line with no area, which is what static geometry is made of.
//
// The neighbours' tangents stay zero. The constructor that takes them is the
// segment-chain one, and until it sets them cp's end-cap rejection is the no-op
// it is in cp, where nothing writes them at all.
func NewSegmentShape(a, b m.Vec2d, radius float64) Shape {
	shape := Shape{
		Radius:        radius,
		CollisionBits: CollisionBitsAll,
		CollidesWith:  CollisionBitsAll,
		Kind:          ShapeSegment,
	}
	shape.Verts[0] = a
	shape.Verts[1] = b
	return shape
}

// NewSegmentShapeWithNeighbours is the segment from a to b, fattened by that
// radius, with the points the app knows come before and after it along its run
// of geometry. Every point is local to the Body's Position.
//
// The two neighbours are kept as the tangents C's cpSegmentShapeSetNeighbors
// stores — previous − a and next − b — at verts[2] and verts[3], and stay local:
// cp's CacheData never transforms them, and the end-cap rejection rotates them
// on use. With them written, a Body rolling along a run of segments does not
// catch at the joints, which is defect 4 fixed. Nothing in cp ever writes them,
// which is why cp's own rejection is dead code.
//
// There is no chain concept. The segments stay separate Entities and which
// segment neighbours which is the app's knowledge, like door axes and merged
// runs.
func NewSegmentShapeWithNeighbours(previous, a, b, next m.Vec2d, radius float64) Shape {
	shape := NewSegmentShape(a, b, radius)
	shape.Verts[2] = previous.Sub(a)
	shape.Verts[3] = next.Sub(b)
	return shape
}

// Offset is a circle's centre, local to the Body's Position. It is meaningless
// on any other kind.
func (s Shape) Offset() m.Vec2d { return s.Verts[0] }

// A is a segment's first endpoint, local to the Body's Position.
func (s Shape) A() m.Vec2d { return s.Verts[0] }

// B is a segment's second endpoint, local to the Body's Position.
func (s Shape) B() m.Vec2d { return s.Verts[1] }

// collides is cp's ShapeFilter.Reject, two-sided and inverted: a pair collides
// when each side is in a group the other looks for. cp's Group is not ported,
// being the collision group filter the app's own relationship filter covers.
func collides(bitsA, collidesWithA, bitsB, collidesWithB uint32) bool {
	return bitsA&collidesWithB != 0 && bitsB&collidesWithA != 0
}
