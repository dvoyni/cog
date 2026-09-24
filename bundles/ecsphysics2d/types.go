package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal"

// Transform is a 2D affine transform, the matrix
//
//	| A C TX |
//	| B D TY |
//
// A Body's is rigid: a rotation about its Position and a translation, never a
// scale. It is physics-only and not m.Transform, which is 3D and would
// put a third axis into this package's contract.
//
// Its methods are Inverse, Mul, Point, Vec and BB; NewTransform and its
// siblings in utils.go build one.
type Transform = internal.Transform

// BB is an axis-aligned bounding box: left, bottom, right, top. It is what the
// indices key on and what a broadphase rejection compares.
//
// Its methods are Intersects, Contains, ContainsVec, Merge, Expand, Centre,
// Area, Offset, SegmentQuery and IntersectsSegment; NewBB and its siblings in
// utils.go build one.
type BB = internal.BB

// ShapeKind is which of the five kinds a Shape is, and it carries the vertex
// count with it: a count field contradicting its kind cannot be spelled.
type ShapeKind = internal.ShapeKind

// The five Shape kinds. A point is ShapeCircle with a Radius of 0, a capsule is
// ShapeSegment with a Radius, and a box is ShapeQuad.
const (
	ShapeCircle  = internal.ShapeCircle
	ShapeSegment = internal.ShapeSegment
	ShapeTri     = internal.ShapeTri
	ShapeQuad    = internal.ShapeQuad
	ShapePoly    = internal.ShapePoly
)

// CollisionBitsAll is every one of the 32 groups and CollisionBitsNone is none
// of them. Both of a Shape's two fields must agree with the other Shape's for a
// pair to collide, so a Shape with no bits on either field collides with
// nothing — which is what a Shape written as a bare literal is.
const (
	CollisionBitsAll  = internal.CollisionBitsAll
	CollisionBitsNone = internal.CollisionBitsNone
)

// Contact is one pair of touching Shapes as the tick found them: the two
// parties, one normal, the material, and up to two points carrying where they
// touch, how deeply and the Impulses the solver spent there.
//
// It is cp's arbiter as one 320-byte struct, scratch included. A is the Sensor;
// otherwise the party that is not Static; otherwise the lower Entity, and the
// Normal is B's surface facing A.
//
// The app reads A, B, Normal, each point's Point, Depth, NormalImpulse and
// TangentImpulse, T, Count, Phase and Sensor; it writes Friction, Restitution,
// SurfaceVelocity and the two marks, all of which last one tick. Before Solve
// the Impulses are the previous tick's and after Solve they are this tick's,
// which is what warm starting means.
//
// Its methods are Dropped, Ignored, Drop, Ignore, Other, NormalFor,
// TotalImpulse and TotalKE.
type Contact = internal.Contact

// ContactPoint is one point of a Contact, 120 bytes. Point is on B's surface
// and Depth is how deeply the two overlap there; both are stored rather than
// derived, because the entry holds an ecs.Entity and a method on it cannot
// reach a position.
type ContactPoint = internal.ContactPoint

// Phase is where a Contact is in its life, and is always on: Began, Continuing
// or Ended.
type Phase = internal.Phase

// The three phases. They are compared against what survived the previous tick's
// filters, so a reacting System always sees a pair begin, continue and end in
// that order however a filter changes its mind between ticks.
const (
	PhaseBegan      = internal.PhaseBegan
	PhaseContinuing = internal.PhaseContinuing
	PhaseEnded      = internal.PhaseEnded
)

// JointKind is which of cp's ten constraints a Joint is, and it names which of
// the parameters mean anything.
type JointKind = internal.JointKind

// The ten Joint kinds. A Spring is the one kind that holds nothing — it only
// pushes — and the motor is the one that drives rather than holds.
const (
	JointPin          = internal.JointPin
	JointSlide        = internal.JointSlide
	JointPivot        = internal.JointPivot
	JointGroove       = internal.JointGroove
	JointSpring       = internal.JointSpring
	JointRotarySpring = internal.JointRotarySpring
	JointRotaryLimit  = internal.JointRotaryLimit
	JointRatchet      = internal.JointRatchet
	JointGear         = internal.JointGear
	JointMotor        = internal.JointMotor
)

// DefaultJointErrorBias is the ErrorBias every Joint constructor fills in: the
// share of positional error left after one second, spelled as a rate per
// second, and byte for byte the same number as the collision bias. MaxForce and
// MaxBias both default to an infinity, which is cp's.
const DefaultJointErrorBias = internal.DefaultJointErrorBias

// Hit is what a Probe reports: the Entity it met, T as the fraction of the
// Probe at which it met it, the Point on that Shape's surface and the unit
// Normal there, facing the Prober.
//
// A Probe that starts overlapping reports T = 0 with the normal from the
// nearest surface point. Snap-back is from.Lerp(to, T).
type Hit = internal.Hit
