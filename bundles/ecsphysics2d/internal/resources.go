package internal

import "github.com/dvoyni/cog/libs/m"

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
type Constants struct {
	// Gravity is the acceleration every Dynamic body receives, in m/s², which
	// is cp's Space gravity term in its velocity integrator. Kinematic and
	// Static bodies never receive it. The default is zero.
	//
	// It is added before the Body's own Force·invMass and scaled by the step
	// with it, so writing m·g into Force instead is the same fall.
	Gravity m.Vec2d
}

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
type Sleep struct {
	// IdleSpeed is how slowly a Dynamic body must be moving to count as idle,
	// in m/s: it is idle on a tick when v·v·m + w²·i, cp's kinetic energy with
	// no ½, is at most m·IdleSpeed².
	//
	// Zero falls back to cp's estimate from gravity, |g|·h, one tick of the
	// Constants' Gravity — so in a world with no gravity an IdleSpeed of 0 never
	// idles anything, and a game that writes its gravity into Force instead
	// names an IdleSpeed itself.
	IdleSpeed float64

	// Time is how long every Body of an Island must stay idle before the Island
	// falls asleep, in seconds. Zero means off, which is the zero-value
	// spelling of cp's infinite SleepTimeThreshold and the default; an infinite
	// Time is off too, as in cp.
	Time float64
}
