package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Solve is the indivisible half of the step, in the nine-step order the
// specification lays out and cp runs:
//
//  1. build the dense solved-Contact list, with cp's four exclusions;
//  2. walk the Joints and build the dense Joint list;
//  3. assign BodyIndex slots over the Bodies in Contacts and in Joints;
//  4. gather those Bodies into the dense arrays;
//  5. PreStep Contacts, then Joints;
//  6. integrate velocities;
//  7. warm start Contacts, then Joints;
//  8. Iterations passes: every Contact, then every Joint, two complete passes
//     an iteration rather than interleaved element by element, as cp does;
//  9. scatter velocities back, and apply the bias as a position delta.
//
// It cannot be split, and both sides force it: PreStep computes the bounce from
// the velocity before integration, which is what stops gravity-fed jitter from
// eating Restitution, and the warm start must follow the damping, or the cached
// Impulse is damped away before it does anything. So the velocity integration
// cannot be a System of its own, and a Force written this tick moves the Body
// next tick.
//
// One step is the ECS layout's rather than cp's. cp integrates the Body the
// solver then reads; the port integrates the Component Store, through the same
// Query that spends and clears the Force, and reads the result back into the
// gathered rows. That is two Store reaches a gathered Body a tick, once, where
// integrating the rows a second time would cost two exponentials each.
func Solve(
	contacts *Contacts,
	bodies *ecs.Query[VelocityQuery],
	joints *ecs.Query[JointQuery],
	dynamics *ecs.Get[Dynamic],
	velocities *ecs.Set[Velocity],
	places *ecs.Set[Position],
	sleeping *ecs.Get[Sleeping],
	gravity m.Vec2d,
	h float64, iterations int, slop, bias float64,
) {
	contacts.beginSolve()
	// Step 2 and its half of step 3: a jointed Body may touch nothing at all,
	// so the gather set is the Bodies in solved Contacts together with the
	// Bodies in Joints, and both are given rows before either is gathered.
	contacts.gatherJoints(joints, places, velocities, sleeping)

	// The gather reads the velocity as it stands before integration, which is
	// the only thing PreStep's bounce can be taken from.
	for i := 1; i < len(contacts.solver.rows); i++ {
		e := contacts.solver.rows[i].entity
		var invMass, invInertia float64
		if body, ok := dynamics.Of(e); ok {
			invMass, invInertia = body.invMass, body.invInertia
		}
		velocity, _ := velocities.Of(e)
		contacts.gatherBody(i, invMass, invInertia, velocity.Linear, velocity.Angular)
	}

	contacts.preStep(h, slop, bias)
	contacts.preStepJoints(h, velocities)

	for _, it := range bodies.All() {
		IntegrateVelocity(&it.Body, it.Velocity, it.Force, gravity, h)
	}
	for i := 1; i < len(contacts.solver.rows); i++ {
		row := &contacts.solver.rows[i]
		if !row.movable() {
			continue
		}
		velocity, _ := velocities.Of(row.entity)
		row.v, row.w = velocity.Linear, velocity.Angular
	}

	coef := contacts.warmStart(h)
	contacts.warmStartJoints(coef)
	contacts.iterate(iterations, h)
	contacts.scatterJoints()

	for i := 1; i < len(contacts.solver.rows); i++ {
		row := &contacts.solver.rows[i]
		if !row.movable() {
			continue
		}
		if velocity, ok := velocities.Ref(row.entity); ok {
			velocity.Linear, velocity.Angular = row.v, row.w
		}
		// The bias never becomes state. cp keeps v_bias and w_bias on the Body
		// and spends them at the top of the next step's position integration;
		// here they are two columns of the gather, applied as a position delta
		// before Solve returns and then discarded. It lands before the app's
		// reaction Systems read Position, so they see the corrected one, and
		// before the next tick's Previous ← Current, so an app that teleports a
		// Body between Solve and Integrate cannot pick up a stale nudge on top
		// of the teleport.
		if row.vBias == (m.Vec2d{}) && row.wBias == 0 {
			continue
		}
		if place, ok := places.Ref(row.entity); ok {
			place.Current = place.Current.Add(row.vBias.MulS(h))
			place.Angle += row.wBias * h
		}
	}
}

// beginSolve is steps 1 and 3 of Solve: it resolves what the filters marked,
// builds the dense solved list, and gives every Body in it a row.
//
// cp builds this list during collision, because its PreSolve is a callback. The
// port builds it at the top of Solve, because the filters are Systems that run
// after Detect — which is also why the marks have to be read here rather than
// acted on where they were made.
//
// Three of cp's four exclusions are applied here: Sensors, the entries a filter
// dropped and the ones it ignores. The fourth, a pair of Bodies that between
// them have no mass, needs the gather to have run and is applied in preStep.
//
// Step 2, the Joint walk, follows in gatherJoints. It amends the earlier shape
// of the gather set, which was the Bodies in solved Contacts: a jointed Body
// may touch nothing at all.
func (c *Contacts) beginSolve() {
	s := &c.solver

	s.solved = s.solved[:0]
	s.rows = append(s.rows[:0], solverBody{})

	// The slot table grows with slack rather than to the exact count. Every other
	// buffer here grows through append and is amortised; sizing this one exactly
	// would allocate every tick of a scene whose Body count climbs by one, which
	// a fixed-size measurement never catches.
	slots := int(c.maxSlot)
	if cap(s.slotDense) < slots {
		s.slotDense = make([]int32, slots, max(2*slots, 64))
	}
	s.slotDense = s.slotDense[:slots]
	for i := range s.slotDense {
		s.slotDense[i] = -1
	}

	for i := range c.current {
		entry := &c.entries[i]
		if entry.Dropped() {
			// A dropped Continuing entry becomes Ended, so that reacting Systems
			// still see the end; a dropped Began entry is one nobody saw begin
			// and stays as it is, to come back as Began next tick.
			//
			// It is rewritten where it stands. The specification orders the
			// Ended entries after every current one, and that is the order Detect
			// writes; a filter marking one afterwards perturbs it, and the
			// alternative — deleting or moving the entry — is rejected outright,
			// because a reacting System would then see a Contact begin twice
			// without ending and every drop would shift the slice under the
			// other filters.
			if entry.Phase == PhaseContinuing {
				entry.Phase = PhaseEnded
			}
		} else if !entry.Sensor && !entry.Ignored() {
			s.assign(c.aux[i].slotA, entry.A)
			s.assign(c.aux[i].slotB, entry.B)
			s.solved = append(s.solved, int32(i))
			continue
		}
		// A tick with no solution has the solution zero. cp says the same by
		// dropping the excluded pair's contacts outright, so its next Update
		// finds none to copy an accumulated impulse from; warm starting carries
		// the previous tick's solution, and the alternative applies a
		// sixty-tick-old Impulse when a filter changes its mind.
		entry.zeroImpulses()
	}
}

// rowsOf is the two rows one solved Contact acts on.
func (c *Contacts) rowsOf(at int32) (*solverBody, *solverBody) {
	s := &c.solver
	aux := c.aux[at]
	first := int32(0)
	if aux.slotA >= 0 {
		first = s.slotDense[aux.slotA]
	}
	second := int32(0)
	if aux.slotB >= 0 {
		second = s.slotDense[aux.slotB]
	}
	return &s.rows[first], &s.rows[second]
}

// gatherBody fills row i with a Body's inverse mass, inverse moment of inertia
// and velocity, as they stand before this tick's velocity integration. A Body
// with no Dynamic is gathered with both inverses zero, which is how a Kinematic
// one pushes what it meets without anything pushing it.
func (c *Contacts) gatherBody(i int, invMass, invInertia float64, linear m.Vec2d, angular float64) {
	row := &c.solver.rows[i]
	row.invMass, row.invInertia = invMass, invInertia
	row.v, row.w = linear, angular
	row.vBias, row.wBias = m.Vec2d{}, 0
}

// preStep is cp's Arbiter.PreStep over every solved Contact, and the place the
// fourth exclusion lands: a pair of Bodies that between them have no mass has
// k_scalar of exactly zero, so it is taken out of the list here rather than
// left to divide by it.
//
// k_scalar is a.m_inv + a.i_inv·(r1×n)² + b.m_inv + b.i_inv·(r2×n)², so it is
// zero only when both Bodies have no inverse mass and no inverse moment — and
// a Body with an inverse moment has a Dynamic, and so has an inverse mass. The
// two Bodies having no mass between them is therefore the whole of the guard,
// and cp has no assertion on this path at all.
//
// bias is the rate per second overlap is pushed out at. cp stores the share of
// overlap left after one second and computes 1 − pow(bias, dt); the port spells
// the same quantity as a rate and computes 1 − exp(−bias·h), which is the same
// function.
func (c *Contacts) preStep(h, slop, bias float64) {
	s := &c.solver
	biasCoef := 1 - math.Exp(-bias*h)

	kept := s.solved[:0]
	for _, at := range s.solved {
		entry := &c.entries[at]
		first, second := c.rowsOf(at)
		if first.invMass == 0 && second.invMass == 0 {
			// The fourth exclusion, and the same rule as the other three: the
			// tick has no solution, so the solution is zero.
			entry.zeroImpulses()
			continue
		}
		kept = append(kept, at)

		normal := entry.Normal
		tangent := normal.Perp()
		for i := range int(entry.Count) {
			point := &entry.Points[i]
			point.nMass = 1 / kScalar(first, second, point.r1, point.r2, normal)
			point.tMass = 1 / kScalar(first, second, point.r1, point.r2, tangent)

			// cp recomputes the separation here as (r2 − r1 + (b.p − a.p))·n.
			// Detection already found it at the same Body positions, the step
			// integrating positions before it detects, so the two are equal by
			// construction and the stored Depth is read instead. cp's
			// −bias·min(0, dist + slop) is this, dist being −Depth.
			point.bias = biasCoef * math.Max(0, point.Depth-slop) / h
			point.jBias = 0

			// The bounce is taken from the velocity before integration, which is
			// what stops gravity-fed jitter from eating Restitution, and is the
			// half of why Solve cannot be split.
			point.bounce = relativeVelocity(first, second, point.r1, point.r2).
				Dot(normal) * entry.Restitution
		}
	}
	s.solved = kept
}

// warmStart is cp's Arbiter.ApplyCachedImpulse: the previous tick's solution
// applied up front, scaled by the ratio of the two steps.
//
// It must follow the velocity integration, or the cached Impulse is damped away
// before it does anything, which is the other half of why Solve cannot be
// split. A Contact that Began is skipped, as cp skips a first contact — a
// revival out of the cached run carries its accumulated Impulses into the
// iterations all the same, which is cp's CACHED to FIRST_COLLISION.
//
// It returns the coefficient it worked out, because the Joints' warm start
// spends the same one and this is where the previous tick's step is spent.
func (c *Contacts) warmStart(h float64) float64 {
	s := &c.solver
	var coef float64
	if s.step != 0 {
		coef = h / s.step
	}
	s.step = h

	for _, at := range s.solved {
		entry := &c.entries[at]
		if entry.Phase == PhaseBegan {
			continue
		}
		first, second := c.rowsOf(at)
		for i := range int(entry.Count) {
			point := &entry.Points[i]
			j := entry.Normal.Rotate(m.Vec2d{X: point.NormalImpulse, Y: point.TangentImpulse})
			applyImpulses(first, second, point.r1, point.r2, j.MulS(coef))
		}
	}
	return coef
}

// iterate is cp's impulse solver: Iterations complete passes over every solved
// Contact and then over every gathered Joint — two complete passes an
// iteration rather than interleaved element by element, as cp does.
func (c *Contacts) iterate(iterations int, h float64) {
	for range iterations {
		for _, at := range c.solver.solved {
			c.applyImpulse(at)
		}
		c.applyJointImpulses(h)
	}
}

// applyImpulse is cp's Arbiter.ApplyImpulse for one Contact.
//
// cp's a and b are this port's B and A: cp's normal points from its first Body
// towards its second, and this port's points from B's surface towards A, so A
// plays the part cp's b plays and the relative velocity is A's at the point
// less B's. Every sign follows from that one substitution and nothing else
// changes.
func (c *Contacts) applyImpulse(at int32) {
	entry := &c.entries[at]
	first, second := c.rowsOf(at)
	normal := entry.Normal
	tangent := normal.Perp()
	friction := entry.Friction

	for i := range int(entry.Count) {
		point := &entry.Points[i]
		r1, r2 := point.r1, point.r2

		biasVelocity := velocityAt(first.vBias, first.wBias, r1).
			Sub(velocityAt(second.vBias, second.wBias, r2))
		relative := relativeVelocity(first, second, r1, r2).Add(entry.SurfaceVelocity)

		biasNormal := biasVelocity.Dot(normal)
		relativeNormal := relative.Dot(normal)
		relativeTangent := relative.Dot(tangent)

		jbn := (point.bias - biasNormal) * point.nMass
		jbnOld := point.jBias
		point.jBias = math.Max(jbnOld+jbn, 0)

		jn := -(point.bounce + relativeNormal) * point.nMass
		jnOld := point.NormalImpulse
		point.NormalImpulse = math.Max(jnOld+jn, 0)

		// The Coulomb clamp. At the shipped Friction of zero jtMax is zero and
		// the tangent is left alone, which is what makes sliding along a wall
		// exactly v ← v − (v·n)·n rather than a rule of its own.
		jtMax := friction * point.NormalImpulse
		jt := -relativeTangent * point.tMass
		jtOld := point.TangentImpulse
		point.TangentImpulse = clamp(jtOld+jt, -jtMax, jtMax)

		applyBiasImpulses(first, second, r1, r2, normal.MulS(point.jBias-jbnOld))
		applyImpulses(first, second, r1, r2, normal.Rotate(m.Vec2d{
			X: point.NormalImpulse - jnOld,
			Y: point.TangentImpulse - jtOld,
		}))
	}
}
