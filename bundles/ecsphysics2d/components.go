package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal"

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
type Position = internal.Position

// Velocity is how fast a Body moves and turns, in metres and radians per
// second. Having one and no Dynamic is what makes a Body Kinematic: the app
// sets its Velocity, it pushes Dynamic bodies, and nothing pushes it.
type Velocity = internal.Velocity

// Force is what gameplay adds to a Dynamic body this tick, in newtons and
// newton-metres. Solve consumes it and clears it, so a Force is said once per
// tick and is never a setting that stays on.
//
// It is a Component of its own so that a System adding Force does not block the
// render copy reading Position.
type Force = internal.Force

// Dynamic is the mass, the Moment of inertia and the two Damping rates of a
// Body that Forces move and what it touches pushes. Having it is what makes a
// Body Dynamic — there is no Kind field and no Kinematic Tag.
//
// Its fields carry an invariant: what is stored is the inverse mass and the
// inverse Moment, so the solver never divides. They are exported so a Dynamic
// serialises whole, and are not written directly. NewDynamicForShape builds one
// from a Shape and a density, recentring the Shape, and NewDynamic from a mass
// and a Moment the app has; SetMass, SetMoment, SetDamping and
// SetAngularDamping change one; Mass and Moment read the inverses back, and
// Damping and AngularDamping are read as fields.
//
// The zero Dynamic is harmless — infinite mass and an infinite Moment, so
// nothing moves it — and is not a Body you built.
type Dynamic = internal.Dynamic

// Static is the Tag of a Body that never moves and is never pushed. It has no
// Velocity, and moving one means replacing the Entity.
type Static = internal.Static

// Sleeping is the Tag of a Dynamic body physics has stopped moving, because it
// and everything in its Island stayed idle for Sleep.Time. The plugin adds it
// and removes it and nothing else may: Integrate, the Body index rebuild and
// the velocity integration skip a Body carrying it, and an app's own Queries
// may skip one the same way, with ecs.Without[Sleeping].
//
// A Sleeping body's Position is bit for bit what it was when it fell asleep. It
// gathers no gravity while it sleeps, so the tick it wakes it receives exactly
// one tick's worth. Its Contacts go quiet — carried unreported, neither
// Continuing nor Ended — and come back Continuing, with their Impulses, when it
// wakes. It is still found by every query on the Body index, and never by one
// on the static index.
//
// Its Island wakes, all of it at once, when:
//
//   - an awake Body touches it, or is jointed to it — a Sensor overlapping it
//     neither wakes it nor stops reporting the overlap;
//   - the app writes its Position or Velocity: a kick, an impulse, a teleport;
//   - its Force differs from the one it carried when it fell asleep. The plugin
//     clears a sleeper's Force every tick, so gravity an app adds into Force
//     every tick, the same value each time, disturbs nothing;
//   - Constants.Gravity changes, which wakes every Island, as cp's SetGravity
//     does;
//   - a member is despawned, or a Static a quiet Contact names leaves the
//     static index — what rested on it falls;
//   - sleeping is turned off, which wakes every Island;
//   - a WakeCmd names it, which is the way for everything else, a Shape or a
//     Dynamic changed while it sleeps among them.
//
// A Kinematic body touching an Island keeps it awake, and a Static or a
// Kinematic body never belongs to an Island nor joins two.
type Sleeping = internal.Sleeping

// Shape is the one convex region a Body occupies: a circle, a segment or a
// convex Polygon, any of them rounded by its Radius. It is a Component the app
// writes, and its Kind names how many of its vertices mean anything.
//
// Its accessors are Offset, A and B; NewCircleShape, NewSegmentShape,
// NewSegmentShapeWithNeighbours, NewPolygonShape, NewBoxShape and
// NewBoxShapeFor in utils.go build one.
//
// Friction and Restitution are plain fields on it, both defaulting to zero as
// cp's do, and a pair's are their plain products. Neither is validated and
// neither constructor takes one, so a material is written onto the Shape after
// it is built.
type Shape = internal.Shape

// Polygon is the vertices of a Shape too large to carry them inline: the second
// Component a Shape of kind ShapePoly needs, on the same Entity beside it.
// Index copies it into the world cache once a tick and nothing else reads it,
// so there is no vertex cap at all.
//
// It is the polygon constructor's second return value, and a Shape of any other
// kind comes back with the zero Polygon. A Polygon written by hand is neither
// hulled nor checked.
type Polygon = internal.Polygon

// Joint is a rule the app sets that holds two Bodies to each other — at a fixed
// distance, about a shared pivot, within a range of Angles, turning in step. It
// is one kind-discriminated Component of 120 bytes carried by an Entity of its
// own, rather than something a Body carries, because one Body may be held by
// several Joints.
//
// It is all ten of cp's constraints in one value: pin, slide, pivot, groove,
// Spring, rotary Spring, rotary limit, ratchet, gear and motor. The parameters
// carry an invariant with Kind, so a Joint is built by one of the ten
// constructors in utils.go and read back through the per-kind accessors, and
// its Params are exported only so it serialises whole, never to be written
// directly; MaxForce, ErrorBias, MaxBias, Kind and CollideBodies are
// plain fields the app writes.
//
// Its accessors are Anchors, GrooveAnchor, Groove, Distance, Span, RestLength,
// RestAngle, Stiffness, Absorption, Angle, Phase, Ratchet, Ratio and Rate, each
// with a setter beside it, and Impulse is what the Joint delivered over the
// tick.
//
// A Joint whose Reference resolves to nothing is skipped and its Impulse reads
// 0: the plugin never despawns a Joint, structural change during the step being
// forbidden, and a dangling Reference is the app's to clean up.
type Joint = internal.Joint
