package internal

import "github.com/dvoyni/cog/bundles/ecs"

// The Query structs the plugin's Systems name. Each one is the lock set of the
// walk it drives, said as a struct: a field per Component, read or written by
// whether it is a pointer, and a Without where a Tag is a filter.
//
// Integrate and Index each name their walk twice, with the Sleeping filter and
// without it, and take the unfiltered one on a tick when nothing sleeps; why is
// on NobodySleeps. The unfiltered twin names a subset of what the
// filtered one does, so the pair locks exactly what the filtered walk alone
// would.

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
	Shape Shape
	Place Position
	_     ecs.Without[Static]
	_     ecs.Without[Sleeping]
}

// everyBodyIndexQuery is bodyIndexQuery without the Sleeping filter: the
// rebuild's walk on a tick when nothing sleeps.
type everyBodyIndexQuery struct {
	Shape Shape
	Place Position
	_     ecs.Without[Static]
}

// jointIndexQuery drives the JointedPairs rebuild: every Joint, read. cp walks
// a Body's intrusive constraint list per candidate pair; the port has no such
// list and will not grow one, so the set of pairs a Joint holds apart is built
// here, once a tick, out of the one walk Index was going to make anyway.
type jointIndexQuery struct {
	Joint Joint
}

// The Query structs the step's Systems name. Each is POD — a field per
// Component, read or written by whether it is a pointer — and each is the lock
// set of the walk it drives, said as a struct.

// VelocityQuery drives the velocity half of Solve: Dynamic bodies only, which
// is cp skipping Kinematic ones, said here by naming Dynamic. Velocity and
// Force are written and Dynamic is read.
//
// A Dynamic body with no Force falls out of this walk and silently never moves.
// That is the one trap in the Component set and it is stated rather than
// checked: the package has no validity checks and does not borrow the ECS's
// Validation mode for them.
//
// A Sleeping body is left out, as cp leaves a sleeping Body out of its
// dynamicBodies: it is not integrated while it sleeps, so it gathers no
// gravity and wakes with exactly one tick of it.
type VelocityQuery struct {
	Velocity *Velocity
	Force    *Force
	Body     Dynamic
	_        ecs.Without[Sleeping]
}

// EveryVelocityQuery is VelocityQuery without the Sleeping filter: the
// velocity integration's walk on a tick when nothing sleeps, which is
// NobodySleeps's to say.
type EveryVelocityQuery struct {
	Velocity *Velocity
	Force    *Force
	Body     Dynamic
}

// JointQuery drives the Joint half of Solve. The Joint is written, because the
// ratchet writes its Angle and every kind writes its Impulse, and because
// writing through the walk is what keeps the write-back off the Store lookup
// path.
type JointQuery struct {
	Joint *Joint
}

// AwakeQuery drives the sleep System's walk over the awake Dynamic bodies,
// where each one's idle time is kept and its node in the tick's Islands is
// given. Everything is read: the mass and the moment for cp's kinetic energy,
// the Velocity it is taken from, and the Force compared against the previous
// tick's.
type AwakeQuery struct {
	Body     Dynamic
	Velocity Velocity
	Force    Force
	_        ecs.Without[Sleeping]
}

// AsleepQuery drives the sleep System's walk over the Sleeping bodies: each
// one's Position, Velocity and Force compared against what the System left in
// them, and the Force cleared when nothing changed. Rest is where what it left
// is kept.
type AsleepQuery struct {
	Place    Position
	Velocity Velocity
	Force    *Force
	Rest     *Rest
	Asleep   Sleeping
}

// IslandJointQuery drives the sleep System's walk over the Joints, which join
// two Dynamic bodies into one Island as a touching pair does. The Joint is
// read: Solve is the one that writes it.
type IslandJointQuery struct {
	Joint Joint
}

// SleeperQuery walks the Sleeping bodies and yields nothing but the Entity. It
// drives NobodySleeps and nothing else.
type SleeperQuery struct {
	_ ecs.With[Sleeping]
}

// NobodySleeps reports that no Entity carries the Sleeping Tag, which is every
// tick of a world with sleeping off and every tick of one with it on until the
// first Island falls asleep.
//
// It is what lets a System walk without the Sleeping filter when the filter
// would drop nothing. A Without is a field of the Query like any other, and
// Query.All picks its walk by field count: it folds the range body into the
// walk at two fields whatever the body's size, at three only while the body is
// small, and past three not at all, so every Entity costs an indirect call to
// the body. The filter takes Integrate's walk from two fields to three, and the
// Body index rebuild's and the velocity integration's from three to four.
// Profiled with sleeping off, it was the largest part of what sleeping cost the
// step. A System that names the walk twice, with the filter and without, and
// takes the unfiltered one here, pays for the filter only while something
// sleeps.
//
// The lock set does not change. The unfiltered walk names a subset of the
// filtered one's Stores, and SleeperQuery's read of the Sleeping Store is the
// read the Without already takes, so a System naming all three locks exactly
// what the filtered walk alone locks.
//
// The answer is exact rather than a hint: the Sleeping Store is read-locked for
// the whole System, so nothing can tag a Body between this and the walk.
func NobodySleeps(sleepers *ecs.Query[SleeperQuery]) bool {
	for range sleepers.All() {
		return false
	}
	return true
}
