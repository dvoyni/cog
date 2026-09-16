package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
)

// positionQuery drives Integrate: every Body with a Velocity, Kinematic ones
// included, exactly as cp integrates positions for everything that is not
// Static. Position is written and Velocity is read, which is the whole of this
// System's lock set beside read{*Entities}.
type positionQuery struct {
	Place    *ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
}

// integrate moves every Body with a Velocity by that Velocity over one step,
// and records where it was when the tick began.
//
// It runs first, which is cp's order and not Box2D's, and is the whole reason a
// Force written this tick moves the Body next tick: the Force this tick's
// gameplay wrote is turned into velocity by Solve, at the end of the same tick,
// and that velocity is spent by the next tick's Integrate. A Velocity written
// directly is not delayed; only Force pays this.
//
// It is one System and not two. Both of the halves anyone would split it into
// use Velocity, one writing and one reading, so a split serialises anyway and
// costs about 6 µs of scheduling to buy nothing.
func integrate(bodies *ecs.Query[positionQuery], step *ecs.In[float64]) {
	// Read once, outside the loop: In is a cell the adapter writes, so a Get
	// inside the loop is a load the compiler cannot hoist.
	h := step.Get()
	for _, it := range bodies.All() {
		types.IntegratePosition(it.Place, &it.Velocity, h)
	}
}

// bodyIndexQuery drives the Body index rebuild: every Entity with a Shape that
// is not Static, Kinematic and Dynamic alike, which is what BodyIndex holds.
// Shape and Position are read and Static is named as a filter, which is the
// whole of this walk's lock set beside read{*Entities}.
//
// Shapeless Bodies are in neither index, and naming Shape as a present field
// rather than a filter is what says so: a Body without one never enters the
// walk.
type bodyIndexQuery struct {
	Shape ecsphysics2d.Shape
	Place ecsphysics2d.Position
	_     ecs.Without[ecsphysics2d.Static]
}

// jointIndexQuery drives the JointedPairs rebuild: every Joint, read. cp walks
// a Body's intrusive constraint list per candidate pair; the port has no such
// list and will not grow one, so the set of pairs a Joint holds apart is built
// here, once a tick, out of the one walk Index was going to make anyway.
type jointIndexQuery struct {
	Joint ecsphysics2d.Joint
}

// index rebuilds the two spatial indices from the positions Integrate has just
// written, drains the Shape hooks that say which Static Entities came and went,
// and rebuilds the set of pairs a Joint holds apart.
//
// The two halves are not symmetric, and that is the whole design. Statics are
// maintained incrementally, world-cached once at Insert and never again, which
// is why moving a Static means replacing the Entity; the Bodies are Cleared and
// refilled whole every tick from wherever Integrate has just put them. Clear
// keeps every buffer it has grown, so the rebuild allocates nothing.
//
// It stays a System of its own rather than folding into Detect, because its
// index writes are held only for the rebuild — about 9 µs for 1 024 Bodies —
// while detection, the heavy part, runs under reads that a gameplay query can
// overlap. One System doing both would hold the index write through detection.
//
// verts is nil at every Insert here: Polygon is a later ticket, and circles and
// segments carry their geometry in the Shape value itself.
func index(
	shapes *ecs.Hooks[ecsphysics2d.Shape, ecs.HookAddedRemoved],
	places *ecs.Get[ecsphysics2d.Position],
	statics *ecs.Get[ecsphysics2d.Static],
	bodies *ecs.Query[bodyIndexQuery],
	joints *ecs.Query[jointIndexQuery],
	staticIndex *ecs.Write[*ecsphysics2d.StaticIndex],
	bodyIndex *ecs.Write[*ecsphysics2d.BodyIndex],
	jointed *ecs.Write[*ecsphysics2d.JointedPairs],
) {
	static := staticIndex.Get()
	for entity, hook := range shapes.All() {
		// The removal is unconditional, and it has to be. A Despawn empties
		// every Store before the record is read, so the Static Tag is already
		// gone by the time this asks, and a removal gated on the Tag would be
		// dropped and leave the Entity in the index for ever. Remove does
		// nothing when the index does not hold the Entity, which is what makes
		// the unconditional call free for every Body's Shape.
		if hook.IsRemoved() {
			static.Remove(entity)
		}
		if hook.IsAdded() {
			// A Hook narrows by nothing but its Component and its kind set, so
			// the Static half of "which index is this Shape's" is checked here,
			// with Get keyed by Entity — the pattern hooks.md's "No filters"
			// prescribes. Without it a Body's Shape would be inserted into the
			// static index at its spawn position and stay there, frozen, while
			// the Body itself moved on in BodyIndex.
			if _, ok := statics.Of(entity); !ok {
				continue
			}
			// A Static whose Shape arrived before its Position is skipped, and
			// skipped silently: there is no second chance, because a Static
			// enters the index on its hook and nowhere else. This is the
			// package's stated-not-checked stance — it has no validity checks
			// and does not borrow the ECS's Validation mode for them — and the
			// way to avoid it is to spawn a Static's Shape, Position and Tag
			// together, which is what an ordinary Spawn does.
			if place, ok := places.Of(entity); ok {
				static.Insert(entity, hook.Value, place.Current, place.Angle, nil)
			}
		}
	}

	body := bodyIndex.Get()
	body.Clear()
	for entity, it := range bodies.All() {
		body.Insert(entity, it.Shape, it.Place.Current, it.Place.Angle, nil)
	}

	// A Joint whose two Bodies still collide contributes nothing, so the set
	// stays empty for every scene that has no such Joint and Detect's check
	// stays one branch. A dangling Reference is added like any other pair: the
	// pair simply never comes up.
	pairs := jointed.Get()
	pairs.Clear()
	for _, it := range joints.All() {
		if !it.Joint.CollideBodies {
			pairs.Add(it.Joint.A, it.Joint.B)
		}
	}
}

// detect finds the tick's Contacts by walking the two indices, and is where the
// seeded coincidence nudge lives.
//
// It names no Component Store. cp's narrowphase reaches the Shape and the Body
// through pointers; the port's index entries carry the Shape, its world cache
// and the transform the position built, so detection reads them and nothing
// else. The lock set is the two indices for read and the Contact list for
// write, which is strictly less than the specification's table allows itself.
//
// An app's filter Systems — cp's Begin and PreSolve — order themselves
// After[DetectOnUpdate]().Before[SolveOnUpdate]() and take
// *ecs.Write[*Contacts].
func (p *plugin) detect(
	staticIndex *ecs.Read[*ecsphysics2d.StaticIndex],
	bodyIndex *ecs.Read[*ecsphysics2d.BodyIndex],
	jointed *ecs.Read[*ecsphysics2d.JointedPairs],
	contacts *ecs.Write[*ecsphysics2d.Contacts],
	step *ecs.In[float64],
) {
	types.Collide(
		contacts.Get(), bodyIndex.Get(), staticIndex.Get(), jointed.Get(),
		p.settings.persistenceTicks(step.Get()),
	)
}

// solve is the indivisible half of the step: the dense solved-Contact list, the
// gather through the BodyIndex slot table, PreStep, the velocity integration,
// the warm start, the iterations, and the bias applied as a position delta.
//
// Velocity integration cannot be a System of its own, which is what makes Solve
// indivisible, and both sides force it: PreStep computes bounce from the
// velocity before integration, which is what stops gravity-fed jitter from
// eating Restitution, and the warm start must follow damping, or the cached
// Impulse is damped away before it does anything.
//
// Position is written here as well as by Integrate, which costs no parallelism:
// the two are links of the same chain, so nothing that could have run beside
// one could have run beside the other.
func (p *plugin) solve(
	bodies *ecs.Query[types.VelocityQuery],
	joints *ecs.Query[types.JointQuery],
	dynamics *ecs.Get[ecsphysics2d.Dynamic],
	velocities *ecs.Set[ecsphysics2d.Velocity],
	places *ecs.Set[ecsphysics2d.Position],
	contacts *ecs.Write[*ecsphysics2d.Contacts],
	step *ecs.In[float64],
) {
	types.Solve(
		contacts.Get(), bodies, joints, dynamics, velocities, places,
		step.Get(), p.settings.iterations, p.settings.slop, p.settings.bias,
	)
}
