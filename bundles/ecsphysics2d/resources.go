package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"

// StaticIndex is the index over the Entities carrying the Static Tag: geometry
// that never changes in place, world-cached once when it is inserted. A System
// queries it through ecs.Read[*StaticIndex].
//
// Its queries are Probe, ProbeAll and Overlap, and Insert, Remove and Clear
// keep it current. Every query is a read and holds no per-query state, so any
// number of them run together and a line of sight never serialises the frame.
//
// It is a Resource of its own type, apart from BodyIndex, so that the locks
// stay apart: rebuilding the Bodies write-locks only the Bodies.
type StaticIndex = types.StaticIndex

// BodyIndex is the index over every other Entity with a Shape, Kinematic and
// Dynamic alike, rebuilt from their positions each tick. Shapeless Bodies are
// in neither index. A System queries it through ecs.Read[*BodyIndex].
//
// It carries the same queries StaticIndex does. Which index a query asks is the
// caller's choice: line of sight against static geometry is a Probe on
// StaticIndex, and "versus Bodies" is a Probe on this one.
type BodyIndex = types.BodyIndex
