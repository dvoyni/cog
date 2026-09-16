package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The whole of the Joint half of Solve: cp's ten constraints ported as one
// pass, slotted into the order Solve already runs in.
//
// cp's a and b are this port's B and A throughout, which is the same
// substitution the Contact solver makes and for the same reason: cp's
// apply_impulses adds the impulse to its second Body, and the port's adds it to
// its first, so cp's b is the party this port calls A. Read every ported line
// with a ↦ B and b ↦ A, cp's r1 ↦ this file's rB and cp's r2 ↦ rA, and every
// sign follows. Nothing else changes.
//
// Joints are a second complete pass after the Contacts in every one of PreStep,
// the warm start and the Iterations passes — never interleaved element by
// element, which is what cp does.

// JointQuery drives the Joint half of Solve. The Joint is written, because the
// ratchet writes its Angle and every kind writes its Impulse, and because
// writing through the walk is what keeps the write-back off the Store lookup
// path.
//
// It is exported for the same reason VelocityQuery is: the System that names it
// is registered a package out, and internal/types is not a package an app can
// reach.
type JointQuery struct {
	Joint *Joint
}

// mat2x2 is cp's Mat2x2, the inverted mass tensor the pivot and the groove
// solve through.
type mat2x2 struct{ a, b, c, d float64 }

func (m2 mat2x2) transform(v m.Vec2d) m.Vec2d {
	return m.Vec2d{X: v.X*m2.a + v.Y*m2.b, Y: v.X*m2.c + v.Y*m2.d}
}

// jointRow is one Joint gathered into the dense list the twelve passes index,
// and the whole of cp's PreStep scratch for every one of the ten kinds.
//
// It is 264 bytes rather than the 128 the specification estimated, and the
// extra is named here rather than discovered later: the two Bodies' places and
// Angles are carried so that the two References are followed exactly once, at
// gather, which was the point of the gather. They are read in PreStep and never
// again.
//
// joint points into the Component Store and is valid only inside one Solve. It
// is set to nil when the row is scattered, so no row outlives the System run
// holding a pointer into a Store — the measured hazard the zero-allocation
// rules name.
type jointRow struct {
	joint *Joint
	// first and second are A's and B's dense Body rows. Row 0 is the immovable
	// one every Body with no Velocity shares, which is how a Joint anchors to
	// the world: a zero-inverse-mass row that is never written back, exactly as
	// a Contact against a wall already is.
	first, second int32
	kind          JointKind
	_             [3]byte

	// rA and rB are the anchors' offsets from A's and B's centres of gravity,
	// which are cp's r2 and r1 in that order.
	rA, rB m.Vec2d
	// n is the pin's, the slide's and the Spring's normal, and the groove's
	// grooveTn — derived at gather rather than stored on the Component.
	n m.Vec2d
	// bias is cp's bias: a vector for the pivot and the groove, and a scalar in
	// X for the other eight.
	bias m.Vec2d
	// jAcc is the accumulated Impulse as the iterations build it, copied back
	// into the Component once, at scatter.
	jAcc m.Vec2d
	// k is the inverted mass tensor the pivot and the groove use.
	k mat2x2

	// mass is cp's nMass for the linear kinds and its iSum for the angular
	// five. The solver never divides in the iterations: this is where the one
	// reciprocal a tick lands.
	mass float64
	// clamp is the groove's clamp, and angle the ratchet's Angle, which is the
	// one parameter that crosses a tick and is written back.
	clamp, angle float64
	// target and coef are either Spring's targetVrn and vCoef, or targetWrn and
	// wCoef.
	target, coef float64
	// param and paramInv are the one parameter ApplyImpulse needs beyond the
	// scratch: the gear's ratio and its inverse, the ratchet's step, the
	// motor's rate. Copying them here is what keeps the iterations off the
	// Component.
	param, paramInv float64
	// maxForce is the Component's, copied for the same reason.
	maxForce float64

	// The two Bodies as the gather found them, read in PreStep and never again.
	posA, posB       m.Vec2d
	angleA, angleB   float64
	entityA, entityB ecs.Entity
}

// jointSolver is the frame-local scratch the Joint passes run over. Every
// buffer is kept across ticks and refilled, so a steady scene allocates
// nothing.
type jointSolver struct {
	rows []jointRow
	// bodies maps an Entity to its dense Body row. It exists because a jointed
	// Body may touch nothing at all and so have no Contact to carry its
	// BodyIndex slot: the slot table answers for the Bodies detection found,
	// and this answers for the rest. It is seeded from the Contact rows and
	// then filled by the walk, and it is built only when the scene has Joints,
	// so a scene with none pays nothing for it.
	bodies entityTable
}

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

// jointSolvable is the guard's three clauses, read off the gathered rows.
func jointSolvable(row *jointRow, first, second *solverBody) bool {
	if first.invMass+second.invMass <= 0 {
		return false
	}
	switch row.kind {
	case JointRotarySpring, JointRotaryLimit, JointRatchet, JointGear, JointMotor:
		if first.invInertia+second.invInertia <= 0 {
			return false
		}
	}
	switch row.kind {
	case JointRatchet, JointGear:
		// The ratchet's step and the gear's ratio each sit in a denominator.
		return row.joint.params[jointThird] != 0
	case JointGroove:
		// A groove whose two ends coincide has no direction, and cp's Project
		// onto it divides zero by zero.
		start, end := row.joint.Groove()
		return start != end
	}
	return true
}

// anchorsOf is cp's two transform.Vect calls: each anchor turned into its
// Body's world-space offset from its centre of gravity. The port's cog is
// fixed at 0, so cp's AnchorA − a.cog is the anchor itself.
func anchorsOf(row *jointRow) (rA, rB m.Vec2d) {
	anchorA, anchorB := row.joint.Anchors()
	return anchorA.Rotate(m.ForAngle(row.angleA)), anchorB.Rotate(m.ForAngle(row.angleB))
}

// jointDelta is cp's delta: the vector from B's anchor to A's, in world space.
func jointDelta(row *jointRow) m.Vec2d {
	return row.posA.Add(row.rA).Sub(row.posB.Add(row.rB))
}

func preStepPin(row *jointRow, first, second *solverBody, coef, h float64) {
	row.rA, row.rB = anchorsOf(row)

	delta := jointDelta(row)
	dist := delta.Length()
	if dist != 0 {
		row.n = delta.MulS(1 / dist)
	} else {
		// cp's own: a divide by an infinity, which is the zero vector.
		row.n = delta.MulS(1 / infinity)
	}

	row.mass = 1 / kScalar(first, second, row.rA, row.rB, row.n)
	maxBias := row.joint.MaxBias
	row.bias.X = clamp(-coef*(dist-row.joint.params[jointFirst])/h, -maxBias, maxBias)
}

func preStepSlide(row *jointRow, first, second *solverBody, coef, h float64) {
	row.rA, row.rB = anchorsOf(row)

	delta := jointDelta(row)
	dist := delta.Length()
	minimum, maximum := row.joint.params[jointFirst], row.joint.params[jointSecond]
	pdist := 0.0
	switch {
	case dist > maximum:
		pdist = dist - maximum
		row.n = delta.Normalize()
	case dist < minimum:
		pdist = minimum - dist
		row.n = delta.Normalize().Negate()
	default:
		// Inside the range the Joint holds nothing, so the tick has no solution
		// and the solution is zero.
		row.n = m.Vec2d{}
		row.jAcc = m.Vec2d{}
	}

	row.mass = 1 / kScalar(first, second, row.rA, row.rB, row.n)
	maxBias := row.joint.MaxBias
	row.bias.X = clamp(-coef*pdist/h, -maxBias, maxBias)
}

func preStepPivot(row *jointRow, first, second *solverBody, coef, h float64) {
	row.rA, row.rB = anchorsOf(row)
	row.k = kTensor(first, second, row.rA, row.rB)
	row.bias = clampLength(jointDelta(row).MulS(-coef/h), row.joint.MaxBias)
}

func preStepGroove(row *jointRow, first, second *solverBody, coef, h float64) {
	start, end := row.joint.Groove()
	// The groove's normal, derived rather than stored: cp caches it in its
	// constructor and never changes it, so this is one normalize a tick and 8
	// bytes off every Joint in the world.
	local := end.Sub(start).Normalize().Perp()

	rotB := m.ForAngle(row.angleB)
	ta := start.Rotate(rotB).Add(row.posB)
	tb := end.Rotate(rotB).Add(row.posB)
	n := local.Rotate(rotB)
	d := ta.Dot(n)

	row.n = n
	row.rA = row.joint.GrooveAnchor().Rotate(m.ForAngle(row.angleA))

	td := row.posA.Add(row.rA).Cross(n)
	switch {
	case td <= ta.Cross(n):
		row.clamp = 1
		row.rB = ta.Sub(row.posB)
	case td >= tb.Cross(n):
		row.clamp = -1
		row.rB = tb.Sub(row.posB)
	default:
		row.clamp = 0
		row.rB = n.Perp().MulS(-td).Add(n.MulS(d)).Sub(row.posB)
	}

	row.k = kTensor(first, second, row.rA, row.rB)
	row.bias = clampLength(jointDelta(row).MulS(-coef/h), row.joint.MaxBias)
}

func preStepSpring(row *jointRow, first, second *solverBody, h float64, velocities *ecs.Set[Velocity]) {
	row.rA, row.rB = anchorsOf(row)

	delta := jointDelta(row)
	dist := delta.Length()
	if dist != 0 {
		row.n = delta.MulS(1 / dist)
	} else {
		row.n = delta.MulS(1 / infinity)
	}

	k := kScalar(first, second, row.rA, row.rB, row.n)
	row.mass = 1 / k
	row.target = 0
	row.coef = 1 - math.Exp(-row.joint.params[jointThird]*h*k)

	// cp's DefaultSpringForce, the linear law, which is the whole of the Spring
	// now that the force hook is gone.
	force := (row.joint.params[jointFirst] - dist) * row.joint.params[jointSecond]
	row.jAcc = m.Vec2d{X: force * h}
	applyJointImpulseToStore(row, first, second, row.n.MulS(row.jAcc.X), velocities)
}

func preStepRotarySpring(row *jointRow, first, second *solverBody, h float64, velocities *ecs.Set[Velocity]) {
	// cp's a.i_inv + b.i_inv, with a ↦ B and b ↦ A.
	moment := second.invInertia + first.invInertia
	row.mass = 1 / moment
	row.coef = 1 - math.Exp(-row.joint.params[jointThird]*h*moment)
	row.target = 0

	// cp's relative angle is a.a − b.a, which is B's Angle less A's.
	torque := (row.angleB - row.angleA - row.joint.params[jointFirst]) * row.joint.params[jointSecond] * h
	row.jAcc = m.Vec2d{X: torque}
	applyJointTorqueToStore(row, first, second, torque, velocities)
}

func preStepRotaryLimit(row *jointRow, first, second *solverBody, coef, h float64) {
	// cp's dist is b.a − a.a, which is A's Angle less B's.
	dist := row.angleA - row.angleB
	minimum, maximum := row.joint.params[jointFirst], row.joint.params[jointSecond]
	pdist := 0.0
	switch {
	case dist > maximum:
		pdist = maximum - dist
	case dist < minimum:
		pdist = minimum - dist
	}

	row.mass = 1 / (second.invInertia + first.invInertia)
	maxBias := row.joint.MaxBias
	row.bias.X = clamp(-coef*pdist/h, -maxBias, maxBias)
	if row.bias.X == 0 {
		row.jAcc = m.Vec2d{}
	}
}

func preStepRatchet(row *jointRow, first, second *solverBody, coef, h float64) {
	angle := row.joint.params[jointFirst]
	phase := row.joint.params[jointSecond]
	ratchet := row.joint.params[jointThird]

	delta := row.angleA - row.angleB
	diff := angle - delta
	pdist := 0.0
	if diff*ratchet > 0 {
		pdist = diff
	} else {
		angle = math.Floor((delta-phase)/ratchet)*ratchet + phase
	}
	// The ratchet's Angle is the one parameter the solver writes. It is carried
	// on the row through the tick and copied back once, at scatter.
	row.angle = angle
	row.param = ratchet

	row.mass = 1 / (second.invInertia + first.invInertia)
	maxBias := row.joint.MaxBias
	row.bias.X = clamp(-coef*pdist/h, -maxBias, maxBias)
	if row.bias.X == 0 {
		row.jAcc = m.Vec2d{}
	}
}

func preStepGear(row *jointRow, first, second *solverBody, coef, h float64) {
	phase := row.joint.params[jointSecond]
	ratio := row.joint.params[jointThird]
	ratioInv := 1 / ratio
	row.param, row.paramInv = ratio, ratioInv

	// The ratio-weighted moment, 1/(a.i_inv·ratio_inv + ratio·b.i_inv). The
	// closed decision record had this as a plain sum; it is corrected here
	// against v2.4.0. The hazard is unchanged — with both inverse moments zero
	// the denominator is still zero whatever the ratio — and the guard above is
	// what catches it.
	row.mass = 1 / (second.invInertia*ratioInv + ratio*first.invInertia)

	maxBias := row.joint.MaxBias
	row.bias.X = clamp(-coef*(row.angleA*ratio-row.angleB-phase)/h, -maxBias, maxBias)
}

func preStepMotor(row *jointRow, first, second *solverBody) {
	row.mass = 1 / (second.invInertia + first.invInertia)
	row.param = row.joint.params[jointThird]
}

// applyJointImpulseToStore is the linear Spring's PreStep Impulse, written into
// the Velocity Components rather than into the gathered rows: the rows are
// re-read from the Stores after the velocity integration, so a row write would
// be thrown away, and the Store write is what puts the Impulse before the
// Damping multiply exactly as cp does.
func applyJointImpulseToStore(
	row *jointRow, first, second *solverBody, j m.Vec2d, velocities *ecs.Set[Velocity],
) {
	if velocity, ok := velocities.Ref(row.entityA); ok {
		velocity.Linear = velocity.Linear.Add(j.MulS(first.invMass))
		velocity.Angular += first.invInertia * row.rA.Cross(j)
	}
	negated := j.Negate()
	if velocity, ok := velocities.Ref(row.entityB); ok {
		velocity.Linear = velocity.Linear.Add(negated.MulS(second.invMass))
		velocity.Angular += second.invInertia * row.rB.Cross(negated)
	}
}

// applyJointTorqueToStore is the same for the rotary Spring, which turns the
// two Bodies rather than pushing them.
func applyJointTorqueToStore(
	row *jointRow, first, second *solverBody, j float64, velocities *ecs.Set[Velocity],
) {
	if velocity, ok := velocities.Ref(row.entityB); ok {
		velocity.Angular -= j * second.invInertia
	}
	if velocity, ok := velocities.Ref(row.entityA); ok {
		velocity.Angular += j * first.invInertia
	}
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

func applyPin(row *jointRow, first, second *solverBody, h float64) {
	vrn := relativeVelocity(first, second, row.rA, row.rB).Dot(row.n)
	jnMax := row.maxForce * h

	jn := (row.bias.X - vrn) * row.mass
	jnOld := row.jAcc.X
	row.jAcc.X = clamp(jnOld+jn, -jnMax, jnMax)

	applyImpulses(first, second, row.rA, row.rB, row.n.MulS(row.jAcc.X-jnOld))
}

func applySlide(row *jointRow, first, second *solverBody, h float64) {
	if row.n == (m.Vec2d{}) {
		return
	}
	vrn := relativeVelocity(first, second, row.rA, row.rB).Dot(row.n)

	jn := (row.bias.X - vrn) * row.mass
	jnOld := row.jAcc.X
	row.jAcc.X = clamp(jnOld+jn, -row.maxForce*h, 0)

	applyImpulses(first, second, row.rA, row.rB, row.n.MulS(row.jAcc.X-jnOld))
}

func applyPivot(row *jointRow, first, second *solverBody, h float64) {
	vr := relativeVelocity(first, second, row.rA, row.rB)

	j := row.k.transform(row.bias.Sub(vr))
	jOld := row.jAcc
	row.jAcc = clampLength(row.jAcc.Add(j), row.maxForce*h)

	applyImpulses(first, second, row.rA, row.rB, row.jAcc.Sub(jOld))
}

func applyGroove(row *jointRow, first, second *solverBody, h float64) {
	vr := relativeVelocity(first, second, row.rA, row.rB)

	j := row.k.transform(row.bias.Sub(vr))
	jOld := row.jAcc
	row.jAcc = grooveConstrain(row, jOld.Add(j), h)

	applyImpulses(first, second, row.rA, row.rB, row.jAcc.Sub(jOld))
}

// grooveConstrain is cp's own: an Impulse that would drive A off the end of the
// groove keeps only its component along the groove's normal.
func grooveConstrain(row *jointRow, j m.Vec2d, h float64) m.Vec2d {
	var clamped m.Vec2d
	if row.clamp*j.Cross(row.n) > 0 {
		clamped = j
	} else {
		clamped = j.Project(row.n)
	}
	return clampLength(clamped, row.maxForce*h)
}

func applySpring(row *jointRow, first, second *solverBody) {
	vrn := relativeVelocity(first, second, row.rA, row.rB).Dot(row.n)

	damp := (row.target - vrn) * row.coef
	row.target = vrn + damp

	j := damp * row.mass
	row.jAcc.X += j
	applyImpulses(first, second, row.rA, row.rB, row.n.MulS(j))
}

func applyRotarySpring(row *jointRow, first, second *solverBody) {
	// cp's wrn is a.w − b.w, which is B's Angular velocity less A's.
	wrn := second.w - first.w

	damp := (row.target - wrn) * row.coef
	row.target = wrn + damp

	j := damp * row.mass
	row.jAcc.X += j
	second.w += j * second.invInertia
	first.w -= j * first.invInertia
}

func applyRotaryLimit(row *jointRow, first, second *solverBody, h float64) {
	if row.bias.X == 0 {
		return
	}
	// cp's wr is b.w − a.w, which is A's Angular velocity less B's.
	wr := first.w - second.w
	jMax := row.maxForce * h

	j := -(row.bias.X + wr) * row.mass
	jOld := row.jAcc.X
	if row.bias.X < 0 {
		row.jAcc.X = clamp(jOld+j, 0, jMax)
	} else {
		row.jAcc.X = clamp(jOld+j, -jMax, 0)
	}
	j = row.jAcc.X - jOld

	second.w -= j * second.invInertia
	first.w += j * first.invInertia
}

func applyRatchet(row *jointRow, first, second *solverBody, h float64) {
	if row.bias.X == 0 {
		return
	}
	wr := first.w - second.w
	ratchet := row.param
	jMax := row.maxForce * h

	j := -(row.bias.X + wr) * row.mass
	jOld := row.jAcc.X
	row.jAcc.X = clamp((jOld+j)*ratchet, 0, jMax*math.Abs(ratchet)) / ratchet
	j = row.jAcc.X - jOld

	second.w -= j * second.invInertia
	first.w += j * first.invInertia
}

func applyGear(row *jointRow, first, second *solverBody, h float64) {
	// cp's wr is b.w·ratio − a.w, which is A's Angular velocity times the ratio
	// less B's.
	wr := first.w*row.param - second.w
	jMax := row.maxForce * h

	j := (row.bias.X - wr) * row.mass
	jOld := row.jAcc.X
	row.jAcc.X = clamp(jOld+j, -jMax, jMax)
	j = row.jAcc.X - jOld

	second.w -= j * second.invInertia * row.paramInv
	first.w += j * first.invInertia
}

func applyMotor(row *jointRow, first, second *solverBody, h float64) {
	// cp's wr is b.w − a.w + Rate.
	wr := first.w - second.w + row.param
	jMax := row.maxForce * h

	j := -wr * row.mass
	jOld := row.jAcc.X
	row.jAcc.X = clamp(jOld+j, -jMax, jMax)
	j = row.jAcc.X - jOld

	second.w -= j * second.invInertia
	first.w += j * first.invInertia
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

// kTensor is cp's k_tensor, unchanged, with cp's a and b as this port's B and
// A: each offset is paired with its own Body's inverse moment, and the sum is
// the same whichever way round the two are named.
//
// cp asserts det != 0 here. The port has no assertion, because the guard in
// preStepJoints is a precondition: k = m_sum·I + A with A positive
// semi-definite, so det is non-zero whenever m_sum is positive.
func kTensor(a, b *solverBody, r1, r2 m.Vec2d) mat2x2 {
	sum := a.invMass + b.invMass

	k11, k12, k21, k22 := sum, 0.0, 0.0, sum

	r1xsq := r1.X * r1.X * a.invInertia
	r1ysq := r1.Y * r1.Y * a.invInertia
	r1nxy := -r1.X * r1.Y * a.invInertia
	k11 += r1ysq
	k12 += r1nxy
	k21 += r1nxy
	k22 += r1xsq

	r2xsq := r2.X * r2.X * b.invInertia
	r2ysq := r2.Y * r2.Y * b.invInertia
	r2nxy := -r2.X * r2.Y * b.invInertia
	k11 += r2ysq
	k12 += r2nxy
	k21 += r2nxy
	k22 += r2xsq

	det := k11*k22 - k12*k21
	detInv := 1.0 / det
	return mat2x2{k22 * detInv, -k12 * detInv, -k21 * detInv, k11 * detInv}
}

// clampLength is cp's Vector.Clamp: the vector, shortened to that length if it
// is longer. At the default infinite ceiling the comparison is false and the
// vector comes back untouched.
func clampLength(v m.Vec2d, length float64) m.Vec2d {
	if v.Dot(v) > length*length {
		return v.Normalize().MulS(length)
	}
	return v
}

// entityTable is the open-addressed map from an Entity to its dense Body row,
// the same shape as the Contact list's pair map and for the same reason: a hash
// over the whole gather ate the win once, so this one is built only when the
// scene has Joints and is probed once per jointed party per tick rather than
// once per party per iteration.
type entityTable struct {
	cells []entityCell
	used  int
}

// entityCell is one cell. A cell naming NoEntity is empty, which a live Entity
// never is: generations start at 1.
type entityCell struct {
	entity ecs.Entity
	at     int32
}

// entityTableMin is where a fresh table starts. It doubles whenever it is half
// full, and never shrinks on its own.
const entityTableMin = 64

// reset empties the table, keeping the memory it has grown.
func (t *entityTable) reset() {
	for i := range t.cells {
		t.cells[i] = entityCell{}
	}
	t.used = 0
}

func (t *entityTable) lookup(e ecs.Entity) (int32, bool) {
	if len(t.cells) == 0 {
		return 0, false
	}
	mask := uint64(len(t.cells) - 1)
	for at := hashEntity(e) & mask; ; at = (at + 1) & mask {
		cell := &t.cells[at]
		switch {
		case cell.entity == ecs.NoEntity:
			return 0, false
		case cell.entity == e:
			return cell.at, true
		}
	}
}

func (t *entityTable) put(e ecs.Entity, at int32) {
	if t.used*2 >= len(t.cells) {
		t.grow()
	}
	mask := uint64(len(t.cells) - 1)
	for i := hashEntity(e) & mask; ; i = (i + 1) & mask {
		cell := &t.cells[i]
		if cell.entity == ecs.NoEntity {
			*cell = entityCell{entity: e, at: at}
			t.used++
			return
		}
		if cell.entity == e {
			cell.at = at
			return
		}
	}
}

func (t *entityTable) grow() {
	size := len(t.cells) * 2
	if size < entityTableMin {
		size = entityTableMin
	}
	old := t.cells
	t.cells = make([]entityCell, size)
	t.used = 0
	for i := range old {
		if old[i].entity != ecs.NoEntity {
			t.put(old[i].entity, old[i].at)
		}
	}
}

// hashEntity is splitmix's finaliser over one Entity id, for the same reason
// hashPair mixes: Entity ids are small dense integers and would cluster.
func hashEntity(e ecs.Entity) uint64 {
	x := uint64(e) * 0x9E3779B97F4A7C15
	x ^= x >> 29
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 32
	return x
}
