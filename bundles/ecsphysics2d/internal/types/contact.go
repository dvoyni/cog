package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Phase is where a Contact is in its life: it Began this tick, it is
// Continuing from the last one, or it Ended and this is the only tick that says
// so.
//
// Phases are always on, and are compared against what survived the previous
// tick's filters, so a reacting System always sees a pair begin, continue and
// end in that order however a filter changes its mind between ticks.
type Phase uint8

const (
	// PhaseBegan is a pair that was not touching, as the list reported it, on
	// the previous tick.
	PhaseBegan Phase = iota
	// PhaseContinuing is a pair that was reported touching on the previous tick.
	PhaseContinuing
	// PhaseEnded is a pair that is no longer touching, reported for one tick.
	// The entry keeps the previous tick's geometry and carries no reason —
	// separation, a despawned party, a removed Shape and a filter's drop all
	// read the same — so it may name an Entity a Store no longer holds.
	//
	// This departs from cp, whose Count() returns 0 once an arbiter is CACHED,
	// so its Separate callback sees no points at all.
	PhaseEnded
)

// The two marks a filter may leave on an entry. Nothing is ever deleted or
// moved: a reacting System would otherwise see a Contact begin twice without
// ending, and every deletion would shift the slice under the other filters.
const (
	// flagDropped takes the pair out of this tick's solution. A dropped
	// Continuing entry becomes Ended, so reacting Systems see the end; a dropped
	// Began entry is one nobody saw begin, and comes back as Began next tick,
	// where cp does not call Begin again.
	flagDropped uint8 = 1 << iota
	// flagIgnored is cp's arb.Ignore: the pair is skipped until it comes apart,
	// which is what a one-way platform needs. It crosses ticks, so while the
	// pair keeps touching its entry arrives already marked, and it ends when the
	// pair misses one tick.
	flagIgnored
	// flagStaticA and flagStaticB are carried only by a quiet entry, and say
	// which of its parties was a Static when it went quiet: a Static has no
	// Rest to say so and no solver slot to say it by once it is quiet.
	flagStaticA
	flagStaticB
	// flagGone is set by Detect on a quiet entry whose Static party has left
	// the static index, so the entry comes back Ended when its Island wakes.
	flagGone
)

// ContactPoint is one point of a Contact, 120 bytes: cp's Contact struct with
// the two fields the ECS layout forces added to it.
//
// cp derives the public contact point as r1 + bodyA.p, because its arbiter
// holds a *Body. This entry holds an ecs.Entity, so a method on it cannot reach
// a position: Point and Depth are stored, and r1 and r2 sit beside them. That
// is 64 B a pair of cp's own redundancy, kept deliberately — re-deriving r1 and
// r2 would cost two vector subtractions a point inside the impulse loop, ten
// iterations a tick.
//
// NormalImpulse and TangentImpulse are cp's jnAcc and jtAcc and are the only
// numbers here that cross a tick; everything from nMass down is scratch PreStep
// rewrites every tick.
type ContactPoint struct {
	// Point is where the pair touches, on B's surface, in world space.
	Point m.Vec2d
	// Depth is how deeply the two overlap at this point, in metres. PreStep
	// reads it rather than recomputing cp's dist: detection ran at the same Body
	// positions, the step integrating positions before it detects, so the two
	// are equal by construction.
	Depth float64
	// NormalImpulse is cp's jnAcc, the accumulated impulse along the Normal. It
	// carries across ticks, which is what warm starting spends. Before Solve it
	// is the previous tick's and after Solve it is this tick's.
	NormalImpulse float64
	// TangentImpulse is cp's jtAcc, the accumulated friction impulse.
	TangentImpulse float64

	// r1 and r2 are the point's offsets from A's and B's centres of gravity,
	// written at detection as cp's Update writes them.
	r1, r2 m.Vec2d

	nMass, tMass float64
	bounce, bias float64
	jBias        float64

	// id says which point of the pair this is across ticks: two vertex indices
	// packed into a uint32, exact where cp mixes shape pointers into a hash and
	// carries the comment that it could trigger false positives. A and B already
	// fix the two Shapes, so the id only has to tell one pair's at most two
	// points apart.
	id uint32
	_  [4]byte
}

// Contact is one pair of touching Shapes, 320 bytes: cp's arbiter as one
// struct, scratch included.
//
// A split into an app-facing entry and a parallel solver-private array — drawn
// along cp's own "survives the tick / rewritten by PreStep" line — was weighed
// and rejected. The scratch lives here, which is also what makes TotalKE a
// method rather than something needing a back-pointer.
//
// Solid Contacts and Sensor Hits share the type; Solve skips Sensor entries.
type Contact struct {
	// A and B are the two parties. A is the Sensor; otherwise the party that is
	// not Static; otherwise the lower Entity. Pairs are matched across ticks as
	// unordered pairs, so a change of A never breaks the phase.
	A, B ecs.Entity
	// Normal is B's surface at the Contact, facing A: the direction A is pushed
	// along to part the two. There is one normal and one convention, flipped
	// once at the boundary of detection; cp's swapped flag is not ported, and
	// neither is the sign wart in TotalImpulse that it caused.
	Normal m.Vec2d
	// SurfaceVelocity is cp's surface_vr, the relative velocity of the two
	// surfaces with its normal component removed. Detect fills it with zero,
	// because a Shape carries no surface velocity; a filter System writes it
	// for one tick, which is how a conveyor is built (the package's Conveyors
	// recipe).
	//
	// It is A's surface velocity less B's, and the sign is worth stating
	// because it is easy to land backwards. cp computes b.surfaceV − a.surfaceV,
	// and this port's A plays the part cp's b plays — the Normal faces A where
	// cp's faces its second Body — so cp's expression is A's less B's here.
	SurfaceVelocity m.Vec2d
	// Points are the Contact's points, of which Count are meaningful.
	Points [2]ContactPoint
	// T is the fraction of the tick at which the pair met. Everything found
	// where the tick ended reports 1; a Probed Sensor's Hit, a fast solid
	// Body's stop and that Body's other Contacts, found where it stopped,
	// report how far through the tick it was.
	T float64
	// Friction is cp's u for this pair, the product of the two Shapes', filled
	// by Detect every tick and writable by a filter for one tick.
	Friction float64
	// Restitution is cp's e for this pair, the product of the two Shapes'.
	Restitution float64

	// gjkId is cp's collisionId, the cached simplex the next tick's GJK warm
	// starts from. It crosses ticks and is zero for the closed forms.
	gjkId uint32

	// Count is how many of Points mean anything.
	Count uint8
	// Phase is where this Contact is in its life.
	Phase Phase
	// Sensor reports that at least one of the two Shapes is a Sensor, which is
	// what keeps the pair out of the solution.
	Sensor bool

	flags uint8
}

// Dropped reports that a filter has taken this pair out of this tick's
// solution.
func (c *Contact) Dropped() bool { return c.flags&flagDropped != 0 }

// Ignored reports that a filter has taken this pair out of the solution until
// it comes apart.
func (c *Contact) Ignored() bool { return c.flags&flagIgnored != 0 }

// Drop takes the pair out of this tick's solution. An Ended entry cannot be
// dropped — there is nothing left to take out of.
func (c *Contact) Drop() {
	if c.Phase != PhaseEnded {
		c.flags |= flagDropped
	}
}

// Ignore is cp's arb.Ignore: it takes the pair out of the solution until the
// two come apart. While they keep touching the entry arrives already marked.
func (c *Contact) Ignore() { c.flags |= flagIgnored }

// Other is the party of the pair that is not e, and NoEntity when e is neither.
func (c *Contact) Other(e ecs.Entity) ecs.Entity {
	switch e {
	case c.A:
		return c.B
	case c.B:
		return c.A
	}
	return ecs.NoEntity
}

// NormalFor is the unit normal along which e is pushed out of the pair, which
// is Normal for A and its negation for B. It is one party's view of the one
// normal, so a reacting System never flips a sign by hand.
func (c *Contact) NormalFor(e ecs.Entity) m.Vec2d {
	if e == c.B {
		return c.Normal.Negate()
	}
	return c.Normal
}

// TotalImpulse is cp's Arbiter.TotalImpulse: the impulse this Contact applied to
// A over the tick, normal and friction together.
//
// cp returns the sum negated unless its swapped flag is set, a wart of the
// per-pair handlers this port does not have. Deleting swapped deletes the wart:
// the sum is the impulse applied to A, which is the party the Normal faces, and
// B's is its negation.
func (c *Contact) TotalImpulse() m.Vec2d {
	var sum m.Vec2d
	for i := range int(c.Count) {
		point := c.Points[i]
		sum = sum.Add(c.Normal.Rotate(m.Vec2d{X: point.NormalImpulse, Y: point.TangentImpulse}))
	}
	return sum
}

// TotalKE is Chipmunk's cpArbiterTotalKE: the kinetic energy the Contact
// removed over the tick, which is what a collision sound or a damage number is
// scaled by.
//
// It has no counterpart in jakecoffman/cp, so this is ported from the C. cp
// computes kinetic energy in three places, none of them per arbiter:
// Body.KineticEnergy (body.go :443), its caller in cp's own sleeping logic
// (space.go :549), and an inline sum in DebugInfo (everything.go :329).
//
// One guard is added: a point PreStep never reached has nMass and tMass of
// zero, and C never meets one because its Count() reports 0 for a cached
// arbiter. This port's Ended entries keep their points, which is a departure
// already stated, so the divide has to be guarded or that departure would hand
// back an infinity.
func (c *Contact) TotalKE() float64 {
	eCoef := (1 - c.Restitution) / (1 + c.Restitution)
	var sum float64
	for i := range int(c.Count) {
		point := c.Points[i]
		if point.nMass != 0 {
			sum += eCoef * point.NormalImpulse * point.NormalImpulse / point.nMass
		}
		if point.tMass != 0 {
			sum += point.TangentImpulse * point.TangentImpulse / point.tMass
		}
	}
	return sum
}

// zeroImpulses forgets the accumulated solution, which is what a tick the
// solver skipped the pair on leaves behind.
func (c *Contact) zeroImpulses() {
	for i := range c.Points {
		c.Points[i].NormalImpulse = 0
		c.Points[i].TangentImpulse = 0
	}
}

// phased reports that the entry was a current one the previous tick that no
// filter dropped, which is what the next tick's phase is compared against.
//
// An ignored entry counts. The two marks differ in exactly this: a drop is for
// one tick, so the filter re-decides every tick and a pair it dropped begins
// again — the one accepted difference from cp, which does not call Begin twice.
// An ignore runs until the pair comes apart, so there is no re-decision to
// report and the pair Continues, which is cp's own IGNORE state persisting.
func (c *Contact) phased() bool {
	return c.Phase != PhaseEnded && c.flags&flagDropped == 0
}

// survived reports that the entry was one the app saw touching and no filter
// marked at all, which is what an Ended entry is produced for: a dropped pair
// was never shown beginning and an ignored one is never seen at all, so neither
// has an end to report.
func (c *Contact) survived() bool {
	return c.phased() && c.flags&flagIgnored == 0
}
