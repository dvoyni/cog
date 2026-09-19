package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
)

// The three Query structs the plugin's Systems name. Each one is the lock set
// of the walk it drives, said as a struct: a field per Component, read or
// written by whether it is a pointer, and a Without where a Tag is a filter.

// positionQuery drives Integrate: every Body with a Velocity, Kinematic ones
// included, exactly as cp integrates positions for everything that is not
// Static. Position is written and Velocity is read, which is the whole of this
// System's lock set beside read{*Entities}.
type positionQuery struct {
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
