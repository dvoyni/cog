package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The Joint half of Solve, as five passes over the Contact list's gather. The
// rows they fill, and cp's ten constraints as the per-kind maths over one row,
// are in jointsolver.go.
//
// Joints are a second complete pass after the Contacts in every one of PreStep,
// the warm start and the Iterations passes — never interleaved element by
// element, which is what cp does.

// gatherJoints is step 2 and its half of step 3: one walk of the Joint Query,
// each pair of References resolved once, and a dense row with a Body row for
// each party.
//
// Three cases are decided here and never inside an iteration. A Reference that
// resolves to nothing — the Body was despawned — skips the Joint and zeroes its
// stored Impulse; cp asserts instead. A party with no Velocity is Static, and
// shares the one immovable row, which is the ordinary anchor to the world. Any
// other party gets a row of its own, Kinematic ones included, so that a
// Kinematic Body pushes what it is jointed to without anything pushing it.
//
// The guard proper — at least one Body with mass, and for the five angular
// kinds at least one that turns — needs the gather to have run and lands in
// preStepJoints, exactly as the Contacts' both-infinite-mass exclusion does.
func (c *Contacts) gatherJoints(
	walk *ecs.Query[JointQuery],
	places *ecs.Set[Position],
	velocities *ecs.Set[Velocity],
) {
	s := &c.solver
	j := &s.joints
	j.rows = j.rows[:0]
	j.bodies.reset()

	seeded := false
	for _, it := range walk.All() {
		joint := it.Joint

		placeA, okA := places.Of(joint.A)
		placeB, okB := places.Of(joint.B)
		if !okA || !okB {
			// A dangling Reference is the app's to clean up, as it is anywhere
			// else, and Impulse reading 0 is how the app notices. The plugin
			// does not despawn the Joint: structural change during the step is
			// forbidden. There is no diagnostic list and no counter Resource.
			joint.impulse = m.Vec2d{}
			continue
		}
		if !seeded {
			// The Contact rows are addressed by BodyIndex slot and this table
			// is addressed by Entity, so the rows detection already made have
			// to be findable here before the walk can reuse one. It is seeded
			// once, and only when a Joint has survived this far.
			for at := 1; at < len(s.rows); at++ {
				j.bodies.put(s.rows[at].entity, int32(at))
			}
			seeded = true
		}

		j.rows = append(j.rows, jointRow{
			joint:   joint,
			first:   s.jointBodyRow(joint.A, velocities),
			second:  s.jointBodyRow(joint.B, velocities),
			kind:    joint.Kind,
			jAcc:    joint.impulse,
			posA:    placeA.Current,
			posB:    placeB.Current,
			angleA:  placeA.Angle,
			angleB:  placeB.Angle,
			entityA: joint.A,
			entityB: joint.B,
		})
	}
}

// preStepJoints is cp's Constraint.PreStep over every gathered Joint, and the
// place the two-clause guard lands — once a tick, never inside an iteration.
//
// The first clause is provable rather than empirical. Both k_scalar and
// k_tensor build k = m_sum·I + A with A a sum of positive semi-definite outer
// products, so k is non-singular whenever m_sum > 0. It is spelled here on
// m_sum itself, the quantity the proof names: for a Dynamic built by
// NewDynamic, which rejects infinite mass, m_sum > 0 is exactly "at least one
// Body is Dynamic", and spelling it this way also covers the zero Dynamic a
// bare literal makes. It covers k_tensor's own assert and the damped Spring's
// "Unsolvable spring" assert alike.
//
// The second clause is new, and it is a defect in cp's Go port that is not
// among the porting index's seven: four of the angular five compute
// iSum = 1/(a.i_inv + b.i_inv) and the gear computes the ratio-weighted
// 1/(a.i_inv·ratio_inv + ratio·b.i_inv), which changes the arithmetic and not
// the hazard. An infinite moment is legal — it is a Body that does not turn —
// and two of them give iSum = +Inf, an Impulse clamped to an infinite jMax
// since MaxForce defaults to ∞, and then Inf·0 = NaN written straight into
// Angular velocity. Chipmunk asserts moment != 0 in the rotary Spring and
// catches one of the five; the Go port dropped the assert in translation and
// catches none. The port's guard covers all five, silently, without asserting.
//
// A third clause is the port's own, and it is the finiteness invariant rather
// than cp: the gear's ratio and the ratchet's step sit in a denominator, so a
// zero one is skipped where cp would produce a NaN. It is the same shape of
// hazard as the second clause, on a parameter instead of on a Body.
//
// The two Springs apply their Impulse here, into the Velocity Stores rather
// than into the gathered rows, because the rows are re-read after the velocity
// integration: writing the row would throw the Impulse away. Into the Store is
// also what reproduces cp, whose PreStep runs before its velocity_func and
// whose Spring Impulse is therefore multiplied by the Body's Damping in the
// same tick.
func (c *Contacts) preStepJoints(h float64, velocities *ecs.Set[Velocity]) {
	s := &c.solver
	j := &s.joints

	gathered := len(j.rows)
	kept := j.rows[:0]
	for i := range j.rows {
		row := &j.rows[i]
		first, second := s.rowsOfJoint(row)
		if !jointSolvable(row, first, second) {
			row.joint.impulse = m.Vec2d{}
			row.joint = nil
			continue
		}
		row.maxForce = row.joint.MaxForce
		// cp stores the share of positional error left after one second and
		// computes 1 − pow(errorBias, dt); the port spells the same quantity as
		// a rate and computes 1 − exp(−rate·h), which is the same function and
		// is how the collision bias is already spelled.
		coef := 1 - math.Exp(-row.joint.ErrorBias*h)
		switch row.kind {
		case JointPin:
			preStepPin(row, first, second, coef, h)
		case JointSlide:
			preStepSlide(row, first, second, coef, h)
		case JointPivot:
			preStepPivot(row, first, second, coef, h)
		case JointGroove:
			preStepGroove(row, first, second, coef, h)
		case JointSpring:
			preStepSpring(row, first, second, h, velocities)
		case JointRotarySpring:
			preStepRotarySpring(row, first, second, h, velocities)
		case JointRotaryLimit:
			preStepRotaryLimit(row, first, second, coef, h)
		case JointRatchet:
			preStepRatchet(row, first, second, coef, h)
		case JointGear:
			preStepGear(row, first, second, coef, h)
		case JointMotor:
			preStepMotor(row, first, second)
		}
		kept = append(kept, *row)
	}
	// A kept row moves down the slice when a row before it was dropped, which
	// leaves its old slot holding a copy of the same pointer past the end. The
	// tail is cleared so that nothing outside the live rows holds a pointer
	// into a Store once Solve has returned.
	for i := len(kept); i < gathered; i++ {
		j.rows[i].joint = nil
	}
	j.rows = kept
}

// warmStartJoints is cp's Constraint.ApplyCachedImpulse over every gathered
// Joint: the previous tick's solution applied up front, scaled by the ratio of
// the two steps, which is the coefficient the Contacts' warm start worked out.
//
// Two kinds are irregular and do nothing here, which is cp's own: the damped
// Spring and the damped rotary Spring have an empty ApplyCachedImpulse and
// rebuild their accumulated Impulse in PreStep, so for those two the stored
// field is a readout only.
func (c *Contacts) warmStartJoints(coef float64) {
	s := &c.solver
	for i := range s.joints.rows {
		row := &s.joints.rows[i]
		first, second := s.rowsOfJoint(row)
		switch row.kind {
		case JointPin, JointSlide:
			applyImpulses(first, second, row.rA, row.rB, row.n.MulS(row.jAcc.X*coef))
		case JointPivot, JointGroove:
			applyImpulses(first, second, row.rA, row.rB, row.jAcc.MulS(coef))
		case JointSpring, JointRotarySpring:
			// Nothing to do here, as cp says.
		case JointGear:
			j := row.jAcc.X * coef
			second.w -= j * second.invInertia * row.paramInv
			first.w += j * first.invInertia
		default:
			j := row.jAcc.X * coef
			second.w -= j * second.invInertia
			first.w += j * first.invInertia
		}
	}
}

// applyJointImpulses is one complete pass of cp's impulse solver over every
// gathered Joint, run after the Contacts' pass rather than interleaved with it.
func (c *Contacts) applyJointImpulses(h float64) {
	s := &c.solver
	for i := range s.joints.rows {
		row := &s.joints.rows[i]
		first, second := s.rowsOfJoint(row)
		switch row.kind {
		case JointPin:
			applyPin(row, first, second, h)
		case JointSlide:
			applySlide(row, first, second, h)
		case JointPivot:
			applyPivot(row, first, second, h)
		case JointGroove:
			applyGroove(row, first, second, h)
		case JointSpring:
			applySpring(row, first, second)
		case JointRotarySpring:
			applyRotarySpring(row, first, second)
		case JointRotaryLimit:
			applyRotaryLimit(row, first, second, h)
		case JointRatchet:
			applyRatchet(row, first, second, h)
		case JointGear:
			applyGear(row, first, second, h)
		case JointMotor:
			applyMotor(row, first, second, h)
		}
	}
}

// scatterJoints copies the two things that cross a tick back into the
// Components — the accumulated Impulse, and the ratchet's Angle — and drops the
// row's pointer into the Store, so that no buffer kept across ticks holds one.
func (c *Contacts) scatterJoints() {
	rows := c.solver.joints.rows
	for i := range rows {
		row := &rows[i]
		row.joint.impulse = row.jAcc
		if row.kind == JointRatchet {
			row.joint.params[jointFirst] = row.angle
		}
		row.joint = nil
	}
}
