package ecsphysics2d

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal"
)

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
type StaticIndex = internal.StaticIndex

// BodyIndex is the index over every other Entity with a Shape, Kinematic and
// Dynamic alike, rebuilt from their positions each tick. Shapeless Bodies are
// in neither index. A System queries it through ecs.Read[*BodyIndex].
//
// It carries the same queries StaticIndex does. Which index a query asks is the
// caller's choice: line of sight against static geometry is a Probe on
// StaticIndex, and "versus Bodies" is a Probe on this one.
type BodyIndex = internal.BodyIndex

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
type Contacts = internal.Contacts

// JointedPairs is the set of Entity pairs whose Joint says the two Bodies do
// not collide, rebuilt by Index from the Joint Query and read by Detect after
// the bit filter and the bounding-box test — so the Contact is never created
// and never reported, which is cp's own semantics.
//
// It is a Resource of its own so that the Joint walk's write is held only for
// the rebuild, and Detect's check is gated on Len, so a scene with no such
// Joint pays one branch. An app reads it through ecs.Read[*JointedPairs]; Len,
// Has, Add and Clear are the whole of it.
type JointedPairs = internal.JointedPairs

// Constants are the physics values that hold for the whole world rather than
// for one Body, which physics reads every tick and a game may change. Gravity
// is the one there is.
//
// The plugin registers them itself, at its own defaults, beside Contacts: an
// app that never writes them has a world with no gravity, which is a top-down
// plane, and pays nothing. Solve reads them through ecs.Read[*Constants], once
// a tick, so a change applies from the next Solve.
//
// Writing them takes ecs.Write[*Constants] in a System of the app's own, and
// that System then runs in series with the Systems that read them — Solve
// among them — on every tick it is subscribed to, whether or not it writes
// anything. That is the price the app chose, so a value set once belongs in a
// System on app.InitEvent rather than in one that runs every tick.
//
// They are not Config, which is fixed when physics starts and is a property of
// the solver or of an index; these are properties of the scene.
type Constants = internal.Constants

// Sleep is whether physics puts Bodies to sleep, and when: cp's
// IdleSpeedThreshold and SleepTimeThreshold. The plugin registers it itself,
// off, so a world whose app never writes it sleeps nothing and the step does
// exactly what it does with no sleeping at all.
//
// A sleeping Island is neither integrated, re-indexed, detected against itself
// or the statics, nor solved, so a settled pile stops costing the step its
// Contacts. What wakes one is on Sleeping.
//
// An app turns it on by writing it from a System of its own through
// ecs.Write[*Sleep] — once from app.InitEvent is the usual way — and the sleep
// System reads it every tick. Turning it off again wakes every Island.
type Sleep = internal.Sleep
