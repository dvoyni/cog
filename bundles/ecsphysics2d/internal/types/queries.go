package types

import "github.com/dvoyni/cog/bundles/ecs"

// The two Query structs the Systems a package out name. Each is POD — a field
// per Component, read or written by whether it is a pointer — and each is the
// lock set of the walk it drives, said as a struct.

// VelocityQuery drives the velocity half of Solve: Dynamic bodies only, which
// is cp skipping Kinematic ones, said here by naming Dynamic. Velocity and
// Force are written and Dynamic is read.
//
// A Dynamic body with no Force falls out of this walk and silently never moves.
// That is the one trap in the Component set and it is stated rather than
// checked: the package has no validity checks and does not borrow the ECS's
// Validation mode for them.
//
// It is exported because the System that names it is registered a package out,
// and internal/types is not a package an app can reach.
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
