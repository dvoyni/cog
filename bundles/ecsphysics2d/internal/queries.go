package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
)

// The Query structs the plugin's Systems name. Each one is the lock set of the
// walk it drives, said as a struct: a field per Component, read or written by
// whether it is a pointer, and a Without where a Tag is a filter.
//
// Integrate and Index each name their walk twice, with the Sleeping filter and
// without it, and take the unfiltered one on a tick when nothing sleeps; why is
// on types.NobodySleeps. The unfiltered twin names a subset of what the
// filtered one does, so the pair locks exactly what the filtered walk alone
// would.

// positionQuery drives Integrate: every Body with a Velocity, Kinematic ones
// included, exactly as cp integrates positions for everything that is not
// Static. Position is written and Velocity is read, which is the whole of this
// System's lock set beside read{*Entities}.
//
// A Sleeping body is left out, as cp leaves it out of the Bodies it integrates:
// its Position stays bit for bit what it was when it fell asleep.
type positionQuery struct {
	Place    *ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	_        ecs.Without[ecsphysics2d.Sleeping]
}

// everyPositionQuery is positionQuery without the Sleeping filter: Integrate's
// walk on a tick when nothing sleeps.
type everyPositionQuery struct {
	Place    *ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
}

// bodyIndexQuery drives the Body index rebuild: every Entity with a Shape that
// is not Static, Kinematic and Dynamic alike, which is what BodyIndex holds.
// Shape and Position are read and Static is named as a filter, which is the
// whole of this walk's lock set beside read{*Entities}.
//
// Shapeless Bodies are in neither index, and naming Shape as a present field
// rather than a filter is what says so: a Body without one never enters the
// walk.
//
// A Sleeping body is left out too: it is kept in the Body index's grid for
// sleepers, which Index moves it into when its Island falls asleep and out of
// when it wakes, and which is not rebuilt every tick.
type bodyIndexQuery struct {
	Shape ecsphysics2d.Shape
	Place ecsphysics2d.Position
	_     ecs.Without[ecsphysics2d.Static]
	_     ecs.Without[ecsphysics2d.Sleeping]
}

// everyBodyIndexQuery is bodyIndexQuery without the Sleeping filter: the
// rebuild's walk on a tick when nothing sleeps.
type everyBodyIndexQuery struct {
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
