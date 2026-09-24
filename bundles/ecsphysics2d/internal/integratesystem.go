package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
)

// positionQuery drives Integrate: every Body with a Velocity, Kinematic ones
// included, exactly as cp integrates positions for everything that is not
// Static. Position is written and Velocity is read, which is the whole of this
// System's lock set beside read{*Entities}.
//
// A Sleeping body is left out, as cp leaves it out of the Bodies it integrates:
// its Position stays bit for bit what it was when it fell asleep.
type positionQuery struct {
	Place    *Position
	Velocity Velocity
	_        ecs.Without[Sleeping]
}

// everyPositionQuery is positionQuery without the Sleeping filter: Integrate's
// walk on a tick when nothing sleeps.
type everyPositionQuery struct {
	Place    *Position
	Velocity Velocity
}

// integrateSystem moves every Body with a Velocity by that Velocity over one
// step, and records where it was when the tick began.
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
//
// It names its walk twice, with the Sleeping filter and without, and walks
// every Body on a tick when nothing sleeps: NobodySleeps says why, and
// why the lock set is the filtered walk's alone.
func integrateSystem(
	bodies *ecs.Query[positionQuery],
	every *ecs.Query[everyPositionQuery],
	sleepers *ecs.Query[SleeperQuery],
	step *ecs.In[float64],
) {
	// Read once, outside the loop: In is a cell the adapter writes, so a Get
	// inside the loop is a load the compiler cannot hoist.
	h := step.Get()
	if NobodySleeps(sleepers) {
		for _, it := range every.All() {
			IntegratePosition(it.Place, &it.Velocity, h)
		}
		return
	}
	for _, it := range bodies.All() {
		IntegratePosition(it.Place, &it.Velocity, h)
	}
}
