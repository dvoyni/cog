package types

import "fmt"

// The three errors a Dynamic body reports, grouped because none of them
// behaves: an Error method renders what the value already holds. The mass
// Component that returns them is in dynamic.go, and the package root
// re-exports all three so that an app can match on them by name.

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
