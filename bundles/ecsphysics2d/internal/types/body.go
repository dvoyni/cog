package types

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/libs/m"
)

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

// Dynamic is the mass, the Moment of inertia and the two Damping rates of a
// Body that Forces move and what it touches pushes. Its presence is what says
// the Body is Dynamic; there is no Kind field and no Kinematic Tag, which is
// what replaces cp's exact comparison of a mass against INFINITY.
//
// Its fields are unexported because they carry an invariant between them: what
// is stored is the inverse mass and the inverse Moment of inertia, so that the
// solver never divides, and neither inverse may be one a bad input produced.
// NewDynamic and the four setters are the only things that write them.
//
// The zero Dynamic is harmless — infinite mass and an infinite Moment of
// inertia, so nothing it is given moves it — and it is documented as not a Body
// you built rather than as a second spelling of Kinematic. Nothing checks for
// it.
type Dynamic struct {
	invMass, invInertia, damping, angularDamping float64
}

// Static is the Tag of a Body that never moves and is never pushed. It has no
// Velocity: moving a Static body means replacing the Entity, which is what lets
// the static index hold entries that never need re-indexing in place.
type Static struct{}

// ErrBadMass reports a mass that is zero, negative, NaN, or infinite.
//
// Zero, negative and NaN would each store an infinite or a NaN inverse mass,
// which is the +Inf·0 = NaN cp's Joints hit. An infinite mass is rejected for a
// different reason: it would store invMass = 0 and so be a Dynamic body nothing
// can push, indistinguishable from a Kinematic one and yet classified as
// Dynamic by its Components. A Body nothing pushes is said by having no Dynamic
// at all.
type ErrBadMass struct{ Mass float64 }

func (e ErrBadMass) Error() string {
	return fmt.Sprintf("ecsphysics2d: mass %v; a Dynamic body's mass is positive and finite, "+
		"and a Body nothing pushes is one with no Dynamic", e.Mass)
}

// ErrBadMoment reports a Moment of inertia that is zero, negative or NaN.
//
// An infinite Moment of inertia is legal and is not an error: it stores
// invInertia = 0 and means a Body that does not turn, which is cp's own
// spelling of 1/∞. There is no FixedRotation Tag, because it would say twice
// what invInertia = 0 says.
type ErrBadMoment struct{ Moment float64 }

func (e ErrBadMoment) Error() string {
	return fmt.Sprintf("ecsphysics2d: moment of inertia %v; it is positive, and an infinite one is "+
		"legal and means a Body that does not turn", e.Moment)
}

// ErrBadDamping reports a Damping rate that is negative, NaN or infinite, and
// says which of the two rates it was.
//
// Damping is a rate per second and the step multiplies velocity by
// exp(−rate·h), so a negative rate amplifies velocity every tick until it is an
// infinity, and a NaN or an infinite rate poisons it in one. Either breaks the
// port's finiteness invariant — no input produces a NaN or an infinity in a
// Component the plugin writes — which is a constraint on the package and not
// only a test of it. A rate of zero is no Damping at all and is legal.
type ErrBadDamping struct {
	Rate float64
	// Angular reports which rate was refused: the one the Angular velocity
	// decays by, or the one the linear velocity decays by.
	Angular bool
}

func (e ErrBadDamping) Error() string {
	which := "damping"
	if e.Angular {
		which = "angular damping"
	}
	return fmt.Sprintf("ecsphysics2d: %s rate %v; a damping rate is per second, finite, and not negative",
		which, e.Rate)
}

// NewDynamic is the Dynamic body a mass, a Moment of inertia and the two
// Damping rates in 1/s describe. A rejected argument yields the zero Dynamic —
// infinite mass and Moment, which moves under nothing — and the error naming
// which argument it was, so a caller that ignores the error gets a Body that
// visibly does nothing rather than one carrying a NaN.
//
// Damping is per Body, for moving and for turning, which is a superset of cp's
// single global damping: cp's behaviour is reproduced by giving every Body the
// same two rates. A spinning wheel and a sliding crate need different ones.
func NewDynamic(mass, moment, damping, angularDamping float64) (Dynamic, error) {
	var body Dynamic
	if err := body.SetMass(mass); err != nil {
		return Dynamic{}, err
	}
	if err := body.SetMoment(moment); err != nil {
		return Dynamic{}, err
	}
	if err := body.SetDamping(damping); err != nil {
		return Dynamic{}, err
	}
	if err := body.SetAngularDamping(angularDamping); err != nil {
		return Dynamic{}, err
	}
	return body, nil
}

// SetMass replaces the mass, in kilograms, leaving the rest of the Body alone.
// It reports ErrBadMass and writes nothing when the mass is not positive and
// finite.
func (d *Dynamic) SetMass(mass float64) error {
	if !(mass > 0) || math.IsInf(mass, 1) {
		return ErrBadMass{Mass: mass}
	}
	d.invMass = 1 / mass
	return nil
}

// SetMoment replaces the Moment of inertia, in kg·m², leaving the rest of the
// Body alone. It reports ErrBadMoment and writes nothing when the Moment is not
// positive; an infinite Moment is accepted and stores an inverse of zero, which
// is a Body that does not turn.
func (d *Dynamic) SetMoment(moment float64) error {
	if !(moment > 0) {
		return ErrBadMoment{Moment: moment}
	}
	if math.IsInf(moment, 1) {
		d.invInertia = 0
		return nil
	}
	d.invInertia = 1 / moment
	return nil
}

// SetDamping replaces the rate, in 1/s, at which the Body's linear velocity
// decays on its own. It reports ErrBadDamping and writes nothing when the rate
// is negative, NaN or infinite.
func (d *Dynamic) SetDamping(rate float64) error {
	if !(rate >= 0) || math.IsInf(rate, 1) {
		return ErrBadDamping{Rate: rate}
	}
	d.damping = rate
	return nil
}

// SetAngularDamping replaces the rate, in 1/s, at which the Body's Angular
// velocity decays on its own. It reports ErrBadDamping on the same terms as
// SetDamping.
func (d *Dynamic) SetAngularDamping(rate float64) error {
	if !(rate >= 0) || math.IsInf(rate, 1) {
		return ErrBadDamping{Rate: rate, Angular: true}
	}
	d.angularDamping = rate
	return nil
}

// Mass is the Body's mass in kilograms, which is +Inf for the zero Dynamic.
func (d *Dynamic) Mass() float64 { return 1 / d.invMass }

// Moment is the Body's Moment of inertia in kg·m², which is +Inf for a Body
// that does not turn and for the zero Dynamic.
func (d *Dynamic) Moment() float64 { return 1 / d.invInertia }

// Damping is the rate in 1/s at which the Body's linear velocity decays.
func (d *Dynamic) Damping() float64 { return d.damping }

// AngularDamping is the rate in 1/s at which the Body's Angular velocity
// decays.
func (d *Dynamic) AngularDamping() float64 { return d.angularDamping }

// IntegrateVelocity is cp's BodyUpdateVelocity for one Dynamic body over a step
// of h seconds, and is the velocity half of Solve. It damps first and then adds
// the step's Force, exactly as cp does, and clears the Force and the Torque:
//
//	v ← v·exp(−damping·h)        + Force·invMass·h
//	w ← w·exp(−angularDamping·h) + Torque·invInertia·h
//
// Three departures from cp, each with its reason:
//
//   - cp multiplies by one damping scalar the Space precomputed as
//     pow(space.damping, dt). The port computes exp(−rate·h) per Body, because
//     Damping is per Body and stated as a rate per second. The two are the same
//     function: cp's damping is e^−rate.
//   - cp's gravity term is absent, because the port ships no gravity. Until it
//     does, an app writes m·g into Force.
//   - cp early-returns for a Kinematic body. The port never reaches one here,
//     because the Query that drives this names Dynamic and a Kinematic body has
//     none — the ECS layout doing what cp's type test did.
//
// Because cp damps exactly but applies Force as a plain Euler step, the
// terminal speed under a constant Force is not F/(mλ) but
//
//	v* = F/(mλ) · λh/(1 − exp(−λh))
//
// which is +13.0% at λ = 15 /s and 60 Hz. cog's step is fixed, so that is a
// constant offset that disappears into tuning. The exact form
// v ← v·e^−λh + (F/m)(1−e^−λh)/λ was weighed and not taken: it costs a divide
// and a λ = 0 branch per Body per tick to buy nothing at a fixed step.
func IntegrateVelocity(body *Dynamic, velocity *Velocity, force *Force, h float64) {
	velocity.Linear = velocity.Linear.MulS(math.Exp(-body.damping * h)).
		Add(force.Force.MulS(body.invMass * h))
	velocity.Angular = velocity.Angular*math.Exp(-body.angularDamping*h) +
		force.Torque*body.invInertia*h
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
