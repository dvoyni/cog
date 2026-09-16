package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"

// Transform is a 2D affine transform, the matrix
//
//	| A C TX |
//	| B D TY |
//
// A Body's is rigid: a rotation about its Position and a translation, never a
// scale. It is physics-only and not ecsscene.Transform, which is 3D and would
// put a third axis into this package's contract.
//
// Its methods are Inverse, Mul, Point, Vec and BB; NewTransform and its
// siblings in utils.go build one.
type Transform = types.Transform

// BB is an axis-aligned bounding box: left, bottom, right, top. It is what the
// indices key on and what a broadphase rejection compares.
//
// Its methods are Intersects, Contains, ContainsVec, Merge, Expand, Centre,
// Area, Offset, SegmentQuery and IntersectsSegment; NewBB and its siblings in
// utils.go build one.
type BB = types.BB

// Shape is the one convex region a Body occupies: a circle, a segment or a
// convex Polygon, any of them rounded by its Radius. It is a Component the app
// writes, and its Kind names how many of its vertices mean anything.
//
// Its accessors are Offset, A and B; NewCircleShape and NewSegmentShape in
// utils.go build one.
type Shape = types.Shape

// ShapeKind is which of the five kinds a Shape is, and it carries the vertex
// count with it: a count field contradicting its kind cannot be spelled.
type ShapeKind = types.ShapeKind

// The five Shape kinds. A point is ShapeCircle with a Radius of 0, a capsule is
// ShapeSegment with a Radius, and a box is ShapeQuad.
const (
	ShapeCircle  = types.ShapeCircle
	ShapeSegment = types.ShapeSegment
	ShapeTri     = types.ShapeTri
	ShapeQuad    = types.ShapeQuad
	ShapePoly    = types.ShapePoly
)

// CollisionBitsAll is every one of the 32 groups and CollisionBitsNone is none
// of them. Both of a Shape's two fields must agree with the other Shape's for a
// pair to collide, so a Shape with no bits on either field collides with
// nothing — which is what a Shape written as a bare literal is.
const (
	CollisionBitsAll  = types.CollisionBitsAll
	CollisionBitsNone = types.CollisionBitsNone
)

// Hit is what a Probe reports: the Entity it met, T as the fraction of the
// Probe at which it met it, the Point on that Shape's surface and the unit
// Normal there, facing the Prober.
//
// A Probe that starts overlapping reports T = 0 with the normal from the
// nearest surface point. Snap-back is from.Lerp(to, T).
type Hit = types.Hit
