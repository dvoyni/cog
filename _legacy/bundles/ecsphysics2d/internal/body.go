package internal

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The Components a Body is made of, and the two integrations that move them.
// Every one of them is a plain value gameplay writes directly, which is why
// they share a file. The one Body Component that is not, Dynamic, whose
// setters validate and store inverses, is in dynamic.go.

// Position is where a Body stands and which way it faces, with the previous
// tick's kept beside it so a render copy interpolates with a plain lerp.
//
// Current is the centre of gravity: the point the Body moves and turns about,
// and the point a Shape's geometry is local to. There is no separate origin and
// no cog field, which is cp with cog fixed at 0, so a Body's transform is a
// rotation about Current and a translation and nothing else.
//
// Angle is in radians, turning from +X towards +Y as in cp, and is never
// wrapped: a lerp between PreviousAngle and Angle then has no seam at ±π.
// Precision is not a concern, because ten turns a second for ten hours is about
// 2.3e6 rad where float64's spacing is 5e-10.
//
// Previous and PreviousAngle are written at the top of the position update, so
// within a tick they name where the Body was when the tick began.
type Position struct {
	Current, Previous    m.Vec2d
	Angle, PreviousAngle float64
}

// Velocity is how fast a Body moves and turns, in metres and radians per
// second. Having one is what makes a Body a Body the integrator moves:
// a Kinematic body is a Velocity the app writes and no Dynamic beside it, and
// a Static body has none at all.
type Velocity struct {
	Linear  m.Vec2d
	Angular float64
}

// Force is what gameplay adds to a Dynamic body this tick and what Solve
// consumes: newtons and newton-metres, cleared to zero once integrated.
//
// It is its own Component rather than a field of Velocity because it is the
// busiest gameplay write and Position's readers must not block on it. A Dynamic
// body without one falls out of the velocity integrator's Query and silently
// never moves.
type Force struct {
	Force  m.Vec2d
	Torque float64
}

// Static is the Tag of a Body that never moves and is never pushed. It has no
// Velocity: moving a Static body means replacing the Entity, which is what lets
// the static index hold entries that never need re-indexing in place.
type Static struct{}

// Sleeping is the Tag of a Dynamic body physics has stopped moving, because it
// and everything in its Island stayed idle long enough. The sleep System adds
// it and removes it and nothing else does; Integrate, the Body index rebuild
// and the velocity integration skip a Body that carries it with
// Without[Sleeping], and an app's own Queries may do the same.
//
// It is a Tag rather than a field because a Body's kind is said by its
// Components, and because adding or removing one takes the write on its own
// Store and nothing wider: only the sleep System writes that Store, so every
// System that filters on it gains a read of a Store nothing beside it in the
// frame writes.
type Sleeping struct{}

// IntegrateVelocity is cp's BodyUpdateVelocity for one Dynamic body over a step
// of h seconds, and is the velocity half of Solve. It damps first and then adds
// the step's gravity and Force together, exactly as cp does, and clears the
// Force and the Torque:
//
//	v ← v·exp(−damping·h)        + (gravity + Force·invMass)·h
//	w ← w·exp(−angularDamping·h) + Torque·invInertia·h
//
// gravity is the Constants' Gravity, read once a tick by Solve and handed to
// every Body; it has no angular term. Because it is added beside Force·invMass
// and scaled by the step with it, an app that writes m·g into Force under a
// gravity of zero gets the same fall.
//
// Two departures from cp, each with its reason:
//
//   - cp multiplies by one damping scalar the Space precomputed as
//     pow(space.damping, dt). The port computes exp(−rate·h) per Body, because
//     Damping is per Body and stated as a rate per second. The two are the same
//     function: cp's damping is e^−rate.
//   - cp early-returns for a Kinematic body. The port never reaches one here,
//     because the Query that drives this names Dynamic and a Kinematic body has
//     none — the ECS layout doing what cp's type test did. So neither a
//     Kinematic nor a Static body receives gravity, as in cp.
//
// Because cp damps exactly but applies Force as a plain Euler step, the
// terminal speed under a constant Force is not F/(mλ) but
//
//	v* = F/(mλ) · λh/(1 − exp(−λh))
//
// which is +13.0% at λ = 15 /s and 60 Hz. cog's step is fixed, so that is a
// constant offset that disappears into tuning. The exact form
// v ← v·e^−λh + (F/m)(1−e^−λh)/λ was weighed and not taken: it costs a divide
// and a λ = 0 branch per Body per tick to buy nothing at a fixed step. Gravity
// is an acceleration applied the same way, so a falling Body's terminal speed
// carries the same factor.
func IntegrateVelocity(body *Dynamic, velocity *Velocity, force *Force, gravity m.Vec2d, h float64) {
	velocity.Linear = velocity.Linear.MulS(math.Exp(-body.Damping * h)).
		Add(gravity.Add(force.Force.MulS(body.InvMass)).MulS(h))
	velocity.Angular = velocity.Angular*math.Exp(-body.AngularDamping*h) +
		force.Torque*body.InvInertia*h
	*force = Force{}
}

// IntegratePosition is cp's BodyUpdatePosition for one Body over a step of h
// seconds, and is the whole of Integrate. It runs for every Body with a
// Velocity, Kinematic ones included, as cp's does.
//
//	Previous ← Current;  PreviousAngle ← Angle
//	Current  ← Current + v·h
//	Angle    ← Angle   + w·h
//
// Two departures from cp, both the ECS layout. cp adds v_bias and w_bias here
// and zeroes them, spending the previous step's de-penetration correction; the
// port never makes the bias a Body's state at all and applies it as a position
// delta before Solve returns. And cp calls SetTransform to cache the Body's
// world transform, where the port derives cos and sin where the world-space
// Shape data is cached instead. Recording Previous and PreviousAngle is the one
// thing added: cp has nowhere to interpolate a render frame from.
func IntegratePosition(position *Position, velocity *Velocity, h float64) {
	position.Previous, position.PreviousAngle = position.Current, position.Angle
	position.Current = position.Current.Add(velocity.Linear.MulS(h))
	position.Angle += velocity.Angular * h
}
