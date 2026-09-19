package types

import "math"

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
