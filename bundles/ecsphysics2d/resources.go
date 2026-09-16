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

// Contacts is the tick's Contact list, one entry per touching pair, written
// once a tick by Detect and always on. A filter System reaches it through
// ecs.Write[*Contacts], ordered After[DetectOnUpdate]().Before[SolveOnUpdate](),
// and a reacting System after Solve.
//
// All is the list and Len is its length; there is no per-Entity index, so
// "what is this Body touching now" is a walk with the one-party view. Its
// contents persist until the next Detect, so a System ordered before Integrate
// legally reads the previous tick's.
//
// The buffer carries a third run past the end of All: pairs that have already
// reported Ended and are carried unreported as Impulse carriers until the
// persistence window closes, so a pair that flickers apart and back keeps its
// Impulses. It is rebuilt each tick reusing its buffers and allocates nothing.
type Contacts = types.Contacts
