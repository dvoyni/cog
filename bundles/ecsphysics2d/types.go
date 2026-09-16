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

// Position is where a Body stands and which way it faces, and is the Component
// every kind of Body has. Current is the centre of gravity — the point the Body
// moves and turns about, and the point a Shape's geometry is local to — and
// Angle is unwrapped radians turning from +X towards +Y.
//
// Previous and PreviousAngle are the previous tick's, written at the top of the
// position update, so a render copy interpolates between them with a plain lerp
// and no seam at ±π.
//
// Integrate writes it, Solve writes the de-penetration correction into it, and
// gameplay writes it to place or teleport a Body.
type Position = types.Position

// Velocity is how fast a Body moves and turns, in metres and radians per
// second. Having one and no Dynamic is what makes a Body Kinematic: the app
// sets its Velocity, it pushes Dynamic bodies, and nothing pushes it.
type Velocity = types.Velocity

// Force is what gameplay adds to a Dynamic body this tick, in newtons and
// newton-metres. Solve consumes it and clears it, so a Force is said once per
// tick and is never a setting that stays on.
//
// It is a Component of its own so that a System adding Force does not block the
// render copy reading Position.
type Force = types.Force

// Dynamic is the mass, the Moment of inertia and the two Damping rates of a
// Body that Forces move and what it touches pushes. Having it is what makes a
// Body Dynamic — there is no Kind field and no Kinematic Tag.
//
// Its fields are unexported because they carry an invariant: what is stored is
// the inverse mass and the inverse Moment, so the solver never divides.
// NewDynamic builds one; SetMass, SetMoment, SetDamping and SetAngularDamping
// change one; Mass, Moment, Damping and AngularDamping read one back.
//
// The zero Dynamic is harmless — infinite mass and an infinite Moment, so
// nothing moves it — and is not a Body you built.
type Dynamic = types.Dynamic

// Static is the Tag of a Body that never moves and is never pushed. It has no
// Velocity, and moving one means replacing the Entity.
type Static = types.Static

// Shape is the one convex region a Body occupies: a circle, a segment or a
// convex Polygon, any of them rounded by its Radius. It is a Component the app
// writes, and its Kind names how many of its vertices mean anything.
//
// Its accessors are Offset, A and B; NewCircleShape and NewSegmentShape in
// utils.go build one.
//
// Friction and Restitution are plain fields on it, both defaulting to zero as
// cp's do, and a pair's are their plain products. Neither is validated and
// neither constructor takes one, so a material is written onto the Shape after
// it is built.
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
type Contact = types.Contact

// ContactPoint is one point of a Contact, 120 bytes. Point is on B's surface
// and Depth is how deeply the two overlap there; both are stored rather than
// derived, because the entry holds an ecs.Entity and a method on it cannot
// reach a position.
type ContactPoint = types.ContactPoint

// Phase is where a Contact is in its life, and is always on: Began, Continuing
// or Ended.
type Phase = types.Phase

// The three phases. They are compared against what survived the previous tick's
// filters, so a reacting System always sees a pair begin, continue and end in
// that order however a filter changes its mind between ticks.
const (
	PhaseBegan      = types.PhaseBegan
	PhaseContinuing = types.PhaseContinuing
	PhaseEnded      = types.PhaseEnded
)

// Hit is what a Probe reports: the Entity it met, T as the fraction of the
// Probe at which it met it, the Point on that Shape's surface and the unit
// Normal there, facing the Prober.
//
// A Probe that starts overlapping reports T = 0 with the normal from the
// nearest surface point. Snap-back is from.Lerp(to, T).
type Hit = types.Hit
