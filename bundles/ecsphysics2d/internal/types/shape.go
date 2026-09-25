package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

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
// per Entity and a kind is not a structural change. The vertices are unexported
// because they carry an invariant with Kind; everything else is a plain field,
// so a Shape is data the app writes.
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
	verts                       [4]m.Vec2d
	Radius                      float64
	Friction                    float64
	Restitution                 float64
	CollisionBits, CollidesWith uint32
	Kind                        ShapeKind
	Sensor                      bool
	// StopsAtBodies says that a fast solid Body of this Shape meets the
	// Kinematic and Dynamic Bodies on its path, and not only the Static ones:
	// it is stopped at them, or carries them when it is Kinematic
	// (continuous-collision.md § The fall-back for a solid Body). Its path test
	// then queries the Body index beside the static index, which is most of
	// what an engaged Body costs, so it is off unless the app sets it. A fast
	// Body without it passes through a Kinematic or Dynamic Body its path
	// meets within one tick, which the discrete walk alone may or may not
	// catch, and does not report a resting Sensor that is a Body. A Sensor
	// ignores it: every moving Sensor is swept against both indices.
	StopsAtBodies bool
	// The byte after StopsAtBodies is spare.
	_ [1]byte
	// faceDistance is a Polygon kind's distance from its Position to the line
	// of its nearest face, which with the rounding radius is its minimum
	// extent. It sits in what was padding, so the Shape stays 104 bytes.
	//
	// It is written by the constructors that write vertices, which are the
	// only writers of vertices there are, and is 0 on every other kind. A
	// Polygon Shape written as a bare literal has 0 too, so continuous
	// collision's gate engages it on any motion: tested more often, never
	// tunnelling, the same class of hazard as a bare literal's collision bits.
	faceDistance float32
}

// MinimumExtent is how thin a Shape is along its thinnest direction, from the
// Body's Position, which is what continuous collision's gate compares a Body's
// movement in the tick against: a circle's radius, a segment's rounding radius,
// and a Polygon kind's distance to its nearest face's line plus its rounding
// radius. The face distance is 0 on every kind that is not a polygon, so the
// sum needs no branch on the kind.
func MinimumExtent(shape Shape) float64 {
	return shape.Radius + float64(shape.faceDistance)
}

// faceDistanceOf is the distance from the local origin, which is the Body's
// Position, to the nearest line through a face of that outline, kept as a
// float32 rounded towards zero so the gate it feeds never engages later than
// the true extent says.
func faceDistanceOf(verts []m.Vec2d) float32 {
	nearest := math.Inf(1)
	for i, a := range verts {
		edge := verts[(i+1)%len(verts)].Sub(a)
		nearest = min(nearest, math.Abs(edge.Cross(a))/edge.Length())
	}
	if math.IsInf(nearest, 1) || math.IsNaN(nearest) {
		return 0
	}
	kept := float32(nearest)
	if float64(kept) > nearest {
		kept = math.Nextafter32(kept, 0)
	}
	return kept
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
	shape.verts[0] = offset
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
	shape.verts[0] = a
	shape.verts[1] = b
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
	shape.verts[2] = previous.Sub(a)
	shape.verts[3] = next.Sub(b)
	return shape
}

// Offset is a circle's centre, local to the Body's Position. It is meaningless
// on any other kind.
func (s Shape) Offset() m.Vec2d { return s.verts[0] }

// A is a segment's first endpoint, local to the Body's Position.
func (s Shape) A() m.Vec2d { return s.verts[0] }

// B is a segment's second endpoint, local to the Body's Position.
func (s Shape) B() m.Vec2d { return s.verts[1] }

// collides is cp's ShapeFilter.Reject, two-sided and inverted: a pair collides
// when each side is in a group the other looks for. cp's Group is not ported,
// being the collision group filter the app's own relationship filter covers.
func collides(bitsA, collidesWithA, bitsB, collidesWithB uint32) bool {
	return bitsA&collidesWithB != 0 && bitsB&collidesWithA != 0
}
