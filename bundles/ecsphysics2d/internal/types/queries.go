package types

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
type VelocityQuery struct {
	Velocity *Velocity
	Force    *Force
	Body     Dynamic
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
