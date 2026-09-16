// Package types declares the concrete types ecsphysics2d's root aliases, and
// the plain functions its forwarders call.
//
// Everything here is a port of Chipmunk. The source is
// github.com/jakecoffman/cp/v2 v2.4.0 (MIT, Copyright (c) 2017 Jake Coffman),
// checked against Chipmunk2D f2f3d66 (MIT, Copyright (c) 2007-2015 Scott
// Lembcke and Howling Moon Software), cited as C. cp's algorithms are reused as
// far as they go; a departure from cp is a finding, stated where it is made,
// and only four reasons are acceptable: the ECS layout, zero allocations,
// per-second units, or a defect in cp.
//
// The vector is m.Vec2d and lives in libs/m, because every Component and query
// exposes it to gameplay code. Transform and BB are physics-only and live here.
//
// Nothing declared here imports the root, which is what keeps the arrangement
// acyclic.
package types
