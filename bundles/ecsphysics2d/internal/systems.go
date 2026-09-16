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

// velocityQuery drives the velocity half of Solve: Dynamic bodies only, which
// is cp skipping Kinematic ones, said here by naming Dynamic. Velocity and
// Force are written and Dynamic is read.
//
// A Dynamic body with no Force falls out of this walk and silently never moves.
// That is the one trap in the Component set and it is stated rather than
// checked: the package has no validity checks and does not borrow the ECS's
// Validation mode for them.
type velocityQuery struct {
	Velocity *ecsphysics2d.Velocity
	Force    *ecsphysics2d.Force
	Body     ecsphysics2d.Dynamic
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

// index rebuilds the two spatial indices from the positions Integrate has just
// written, and drains the Shape hooks that say which Static Entities came and
// went.
//
// It is empty until there are indices to rebuild. It is registered and chained
// now so that the order never changes when they arrive: an ordering an app has
// already written against IndexOnUpdate keeps meaning what it meant.
//
// It stays a System of its own rather than folding into Detect, because its
// index writes are held only for the rebuild — about 9 µs for 1 024 Bodies —
// while detection, the heavy part, runs under reads that a gameplay query can
// overlap. One System doing both would hold the index write through detection.
func index() {}

// detect finds the tick's Contacts by walking the indices, and is where the
// seeded coincidence nudge lives.
//
// It is empty until there are indices to walk, and is registered and chained
// now for the same reason index is: an app's filter Systems order themselves
// After[DetectOnUpdate]().Before[SolveOnUpdate](), and that ordering is written
// before there is anything to filter.
func detect() {}

// solve is the indivisible half of the step. Today it is the velocity
// integration alone; around it will come the dense solved-Contact list, the
// Joint list, the gather through the slot table, PreStep, the warm start, the
// iterations and the bias applied as a position delta.
//
// Velocity integration cannot be a System of its own, which is what makes Solve
// indivisible, and both sides force it: PreStep computes bounce from the
// velocity before integration, which is what stops gravity-fed jitter from
// eating Restitution, and ApplyCachedImpulse must follow damping, or the
// warm-start Impulse is damped away before it does anything.
func solve(bodies *ecs.Query[velocityQuery], step *ecs.In[float64]) {
	h := step.Get()
	for _, it := range bodies.All() {
		types.IntegrateVelocity(&it.Body, it.Velocity, it.Force, h)
	}
}
