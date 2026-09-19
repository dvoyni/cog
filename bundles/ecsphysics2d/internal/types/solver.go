package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The dense gather both halves of Solve run over: a row per Body, and the slot
// table that hands a Body its row. The Contact passes that fill and drain it
// are contacts-solve.go, the Joint passes contacts-joints.go, and the Joint's
// own rows jointsolver.go.

// solverBody is one Body gathered into the dense arrays the impulse loop runs
// over, so the hot loop touches nothing but this slice.
//
// The Contact entry's two parties are already BodyIndex slots at detection, so
// a []int32 of slot to dense row replaces any hash: at N=1024 a hash over the
// gather ate the whole win, and reaching the Component Stores per Contact per
// iteration cost 12 to 35 µs a tick against a 284 µs step.
//
// vBias and wBias are cp's v_bias and w_bias and live here for free. cp keeps
// them on the Body, spends them at the top of the next step's position
// integration and zeroes them there; the port applies them as a position delta
// before Solve returns and discards them, so there is no Component, no Resource
// and no cross-tick state.
type solverBody struct {
	entity     ecs.Entity
	invMass    float64
	invInertia float64
	v          m.Vec2d
	w          float64
	vBias      m.Vec2d
	wBias      float64
}

// movable reports that an impulse can change this row, which is what says
// whether it is worth writing back.
func (b *solverBody) movable() bool { return b.invMass != 0 || b.invInertia != 0 }

// solver is the frame-local scratch Solve runs over: the dense list of the
// Contacts it will solve, the slot table that assigns Bodies dense rows, and
// the rows themselves. Every buffer is kept across ticks and refilled, so a
// steady scene allocates nothing.
type solver struct {
	// solved indexes the Contact entries that will be solved, with cp's four
	// exclusions already applied. It exists so the Iterations passes do not walk
	// Sensor and dropped entries ten times over.
	solved []int32
	// slotDense maps a BodyIndex slot to a row, and is −1 where the slot has no
	// row yet. It is cleared each tick.
	slotDense []int32
	// rows is the gather. Row 0 is the immovable one every Static party shares:
	// it has no mass, no moment and no velocity, so every impulse applied to it
	// is a multiplication by zero and it never changes.
	rows []solverBody
	// step is the previous tick's step in seconds, which scales the warm start
	// exactly as cp's dt/prev_dt does. It is 0 before the first solve, which is
	// cp's own value there.
	step float64
	// joints is the Joint half of the gather, in jointsolve.go.
	joints jointSolver
}

// assign is the slot table: the dense row a Body has, made if it has none. A
// slot of −1 is a Static party, which shares the one immovable row however many
// Static Bodies the tick touched.
func (s *solver) assign(slot int32, e ecs.Entity) int32 {
	if slot < 0 {
		return 0
	}
	if at := s.slotDense[slot]; at >= 0 {
		return at
	}
	at := int32(len(s.rows))
	s.rows = append(s.rows, solverBody{entity: e})
	s.slotDense[slot] = at
	return at
}

// jointBodyRow is the dense row a jointed Body has, made if it has none. A Body
// with no Velocity never moves and is never pushed, so it shares the one
// immovable row however many Joints reach it.
func (s *solver) jointBodyRow(e ecs.Entity, velocities *ecs.Set[Velocity]) int32 {
	if _, ok := velocities.Of(e); !ok {
		return 0
	}
	if at, ok := s.joints.bodies.lookup(e); ok {
		return at
	}
	at := int32(len(s.rows))
	s.rows = append(s.rows, solverBody{entity: e})
	s.joints.bodies.put(e, at)
	return at
}

// rowsOfJoint is the two Body rows one Joint acts on, A's first.
func (s *solver) rowsOfJoint(row *jointRow) (*solverBody, *solverBody) {
	return &s.rows[row.first], &s.rows[row.second]
}

// kScalarBody and kScalar are cp's own, unchanged.
func kScalarBody(body *solverBody, r, n m.Vec2d) float64 {
	rcn := r.Cross(n)
	return body.invMass + body.invInertia*rcn*rcn
}

func kScalar(a, b *solverBody, r1, r2, n m.Vec2d) float64 {
	return kScalarBody(a, r1, n) + kScalarBody(b, r2, n)
}

// velocityAt is a Body's velocity at a point offset from its centre of gravity.
func velocityAt(linear m.Vec2d, angular float64, r m.Vec2d) m.Vec2d {
	return linear.Add(r.Perp().MulS(angular))
}

// relativeVelocity is cp's relative_velocity with cp's a and b as this port's B
// and A: the velocity of A at its point less the velocity of B at its point,
// which is the quantity the Normal — B's surface facing A — is signed against.
func relativeVelocity(a, b *solverBody, r1, r2 m.Vec2d) m.Vec2d {
	return velocityAt(a.v, a.w, r1).Sub(velocityAt(b.v, b.w, r2))
}

// applyImpulses is cp's apply_impulses: the impulse to A at r1 and its negation
// to B at r2.
func applyImpulses(a, b *solverBody, r1, r2, j m.Vec2d) {
	a.v = a.v.Add(j.MulS(a.invMass))
	a.w += a.invInertia * r1.Cross(j)
	negated := j.Negate()
	b.v = b.v.Add(negated.MulS(b.invMass))
	b.w += b.invInertia * r2.Cross(negated)
}

// applyBiasImpulses is cp's apply_bias_impulses, the same into the bias
// columns.
func applyBiasImpulses(a, b *solverBody, r1, r2, j m.Vec2d) {
	a.vBias = a.vBias.Add(j.MulS(a.invMass))
	a.wBias += a.invInertia * r1.Cross(j)
	negated := j.Negate()
	b.vBias = b.vBias.Add(negated.MulS(b.invMass))
	b.wBias += b.invInertia * r2.Cross(negated)
}

// clamp is cp's Clamp.
func clamp(f, low, high float64) float64 { return math.Min(math.Max(f, low), high) }
