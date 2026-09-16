package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// What the two math.Exp calls exponential Damping added cost, per Body per
// tick. No ticket ever measured them: the specification records exp(−rate·h) as
// exact and stable at any step, notes that Box2D's 1/(1 + rate·h) is cheaper
// and departs from cp, and marks the cost a Gap.
//
// The three arms below are the same velocity update over the same Bodies, and
// differ only in how the damping factor is reached: cp's own exponential, the
// Box2D form the spec names as the cheap alternative, and no damping at all as
// the floor. The arithmetic around them is identical, so the differences are
// the two calls and nothing else.
//
// The Bodies carry different damping rates and are walked in turn rather than
// integrated one over and over, because a single Body's rate is loop-invariant
// and the two calls would be hoisted clean out of the measurement. ns/op is per
// Body per tick.
//
// Every arm writes the Body's Force back before it integrates, and that is not
// decoration. IntegrateVelocity spends the Force and zeroes it, so a Body left
// alone decays geometrically towards zero; over a benchmark's tens of millions
// of iterations it reaches denormals, where the multiply costs many times what
// it costs on an ordinary number — measured at about 3× on this machine, with
// the damped arms inflated and the undamped one, whose velocity never changes,
// left alone. Re-applying the Force holds every arm at its terminal speed,
// which is where a real Body under a real Force sits. The write is the same in
// all three, so the differences are still the damping and nothing else.

// dampedBodies is a power of two so the walk's index is a mask rather than a
// division, which would otherwise be measured beside the exponentials.
const dampedBodies = 1024

// steadyForce is what every arm writes back before it integrates, so that the
// Bodies sit at their terminal speeds rather than decaying into denormals.
var steadyForce = Force{Force: m.Vec2d{X: 10}, Torque: 2}

// dampingScene is a run of Bodies under different damping rates, with the
// Velocity and Force runs beside them.
func dampingScene(tb testing.TB) ([]Dynamic, []Velocity, []Force) {
	tb.Helper()
	bodies := make([]Dynamic, dampedBodies)
	velocities := make([]Velocity, dampedBodies)
	forces := make([]Force, dampedBodies)
	for i := range bodies {
		body, err := NewDynamic(2, 8, 1+float64(i%32)*0.5, 0.1+float64(i%16)*0.25)
		if err != nil {
			tb.Fatalf("building the %dth Body: %v", i, err)
		}
		bodies[i] = body
		velocities[i] = Velocity{Linear: m.Vec2d{X: 3, Y: -1}, Angular: 0.7}
		forces[i] = steadyForce
	}
	return bodies, velocities, forces
}

// BenchmarkVelocityIntegration is what ships: cp's exponential damping, two
// math.Exp calls a Body a tick.
func BenchmarkVelocityIntegration(b *testing.B) {
	bodies, velocities, forces := dampingScene(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := i & (dampedBodies - 1)
		forces[at] = steadyForce
		IntegrateVelocity(&bodies[at], &velocities[at], &forces[at], 1.0/60)
	}
}

// BenchmarkVelocityIntegrationWithBox2DsDampingInstead is the alternative the
// specification names and rejects: 1/(1 + rate·h), which is a divide where the
// exponential is a call. It is here to price the two calls and for no other
// reason — the port keeps cp's form, which is exact at any step where this one
// is not.
func BenchmarkVelocityIntegrationWithBox2DsDampingInstead(b *testing.B) {
	bodies, velocities, forces := dampingScene(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := i & (dampedBodies - 1)
		forces[at] = steadyForce
		integrateVelocityBox2D(&bodies[at], &velocities[at], &forces[at], 1.0/60)
	}
}

// BenchmarkVelocityIntegrationWithNoDampingAtAll is the floor: the same update
// with the damping factor gone, so the two calls are priced against nothing as
// well as against the cheap form.
func BenchmarkVelocityIntegrationWithNoDampingAtAll(b *testing.B) {
	bodies, velocities, forces := dampingScene(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := i & (dampedBodies - 1)
		forces[at] = steadyForce
		integrateVelocityUndamped(&bodies[at], &velocities[at], &forces[at], 1.0/60)
	}
}

// integrateVelocityBox2D is IntegrateVelocity with Box2D's damping in place of
// cp's. It exists in this file alone and nothing in the package calls it.
func integrateVelocityBox2D(body *Dynamic, velocity *Velocity, force *Force, h float64) {
	velocity.Linear = velocity.Linear.MulS(1/(1+body.damping*h)).
		Add(force.Force.MulS(body.invMass * h))
	velocity.Angular = velocity.Angular/(1+body.angularDamping*h) +
		force.Torque*body.invInertia*h
	*force = Force{}
}

// integrateVelocityUndamped is IntegrateVelocity with no damping factor at all.
func integrateVelocityUndamped(body *Dynamic, velocity *Velocity, force *Force, h float64) {
	velocity.Linear = velocity.Linear.Add(force.Force.MulS(body.invMass * h))
	velocity.Angular += force.Torque * body.invInertia * h
	*force = Force{}
}

// BenchmarkTwoExponentialsAlone is the pair of calls with nothing around them,
// over the same spread of rates.
//
// It is a latency and not the cost, and it reads higher than the whole velocity
// update above for that reason: the two calls feed one multiply into a global
// on every iteration, so nothing overlaps them, where in the update they sit
// beside arithmetic the processor runs underneath them. The number to quote for
// what exponential Damping added is the difference between the three arms
// above; this is here to show that the difference is the calls and not
// something around them.
func BenchmarkTwoExponentialsAlone(b *testing.B) {
	bodies, _, _ := dampingScene(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := i & (dampedBodies - 1)
		floatSink = math.Exp(-bodies[at].damping/60) * math.Exp(-bodies[at].angularDamping/60)
	}
}
