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
