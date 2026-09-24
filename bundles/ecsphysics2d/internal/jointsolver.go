package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// cp's ten constraints as the maths over one gathered row: the row, the
// scratch the passes run over, and a PreStep and an apply for each kind. The
// five passes that call them, and the gather that fills the rows, are in
// contacts-joints.go.
//
// cp's a and b are this port's B and A throughout, which is the same
// substitution the Contact solver makes and for the same reason: cp's
// apply_impulses adds the impulse to its second Body, and the port's adds it to
// its first, so cp's b is the party this port calls A. Read every ported line
// with a ↦ B and b ↦ A, cp's r1 ↦ this file's rB and cp's r2 ↦ rA, and every
// sign follows. Nothing else changes.

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
		return row.joint.Params[jointThird] != 0
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
	row.bias.X = clamp(-coef*(dist-row.joint.Params[jointFirst])/h, -maxBias, maxBias)
}

func preStepSlide(row *jointRow, first, second *solverBody, coef, h float64) {
	row.rA, row.rB = anchorsOf(row)

	delta := jointDelta(row)
	dist := delta.Length()
	minimum, maximum := row.joint.Params[jointFirst], row.joint.Params[jointSecond]
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
	row.coef = 1 - math.Exp(-row.joint.Params[jointThird]*h*k)

	// cp's DefaultSpringForce, the linear law, which is the whole of the Spring
	// now that the force hook is gone.
	force := (row.joint.Params[jointFirst] - dist) * row.joint.Params[jointSecond]
	row.jAcc = m.Vec2d{X: force * h}
	applyJointImpulseToStore(row, first, second, row.n.MulS(row.jAcc.X), velocities)
}

func preStepRotarySpring(row *jointRow, first, second *solverBody, h float64, velocities *ecs.Set[Velocity]) {
	// cp's a.i_inv + b.i_inv, with a ↦ B and b ↦ A.
	moment := second.invInertia + first.invInertia
	row.mass = 1 / moment
	row.coef = 1 - math.Exp(-row.joint.Params[jointThird]*h*moment)
	row.target = 0

	// cp's relative angle is a.a − b.a, which is B's Angle less A's.
	torque := (row.angleB - row.angleA - row.joint.Params[jointFirst]) * row.joint.Params[jointSecond] * h
	row.jAcc = m.Vec2d{X: torque}
	applyJointTorqueToStore(row, first, second, torque, velocities)
}

func preStepRotaryLimit(row *jointRow, first, second *solverBody, coef, h float64) {
	// cp's dist is b.a − a.a, which is A's Angle less B's.
	dist := row.angleA - row.angleB
	minimum, maximum := row.joint.Params[jointFirst], row.joint.Params[jointSecond]
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
	angle := row.joint.Params[jointFirst]
	phase := row.joint.Params[jointSecond]
	ratchet := row.joint.Params[jointThird]

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
	phase := row.joint.Params[jointSecond]
	ratio := row.joint.Params[jointThird]
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
	row.param = row.joint.Params[jointThird]
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
