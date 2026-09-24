package internal

import (
	"github.com/dvoyni/cog/kernel"
)

// ShrinkCmd gives physics' memory back after a spike, and it is the only thing
// in the package that does. The Contact buffers, the pair tables, the two grids,
// the world-cache slab and the solver's scratch all keep their high-water
// capacity, so a steady
// scene never allocates; after a level load, a boss's debris or a screen of
// projectiles that capacity stays until the app executes this:
//
//	response, err := executioner.ExecuteCommand[ecsphysics2d.ShrinkCmd](
//	    ecsphysics2d.ShrinkRequest{})
//
// The zero request shrinks every area, and each Keep option opts one out. The
// response reports the bytes each area released; Go frees the old arrays at its
// next collection, and debug.FreeOSMemory is the caller's to call. The ticks
// after it regrow what they need, and those ticks allocate, so shrink when a
// spike has ended rather than every tick.
//
// It is a command and not a heuristic, deliberately. A buffer that shrank
// itself whenever a tick looked quiet would put an allocation into the tick
// after every lull, on the hot path, with nothing in the app able to say when;
// and a buffer that never gave memory back would hold the busiest scene's peak
// for the life of the world. Both were rejected. The app knows when the spike
// is over and nothing else does.
//
// The physics plugin registers it, holding write on Contacts, StaticIndex and
// BodyIndex and nothing besides — no Component Store and not the id authority.
// That excludes Index, Detect and Solve while it runs, which is exactly what it
// must, and costs a tick that does not execute it nothing at all. It also
// declares itself exclusive, so two invocations never overlap whatever its
// locks become.
type ShrinkCmd kernel.Command[ShrinkRequest, ShrinkResponse]

// WakeCmd wakes the Island of the Entity it names, for whatever disturbs a
// Sleeping body that is not a touch, a Position, Velocity or Force written, a
// changed gravity or a support removed — each of which wakes one on its own.
// A Shape or a Dynamic changed while a Body sleeps is the usual reason: a
// sleeper is not re-indexed and not re-read until it wakes.
//
//	executioner.ExecuteCommand[ecsphysics2d.WakeCmd](
//	    ecsphysics2d.WakeRequest{Entity: crate})
//
// The Island wakes on the sleep System's next run, which is before Solve on the
// tick it is executed in when it runs Before[IntegrateOnUpdate]. An Entity that
// is not asleep is left as it is.
//
// The physics plugin registers it, holding write on its own queue and nothing
// besides, so a System dispatching it serialises with the sleep System and with
// nothing else. It declares itself exclusive, as ShrinkCmd does.
type WakeCmd kernel.Command[WakeRequest, WakeResponse]
