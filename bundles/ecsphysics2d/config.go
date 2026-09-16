package ecsphysics2d

// Config is the plugin's settings, keyed by Name in the engine's configuration
// map and fixed at registration. A zero field takes its default, so a caller
// names only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{ecsphysics2d.Name: ecsphysics2d.Config{Slop: 0.01}})
//
// These are configuration rather than constants, and yet nothing changes them
// at runtime: cp exposes setters for its own and never moves them mid-run, and
// every one of these is a property of the solver or of an index rather than of
// a scene. There is deliberately no settings Resource and no settings command.
// A Resource an app System declared write on would conflict with Solve for the
// whole frame, on every tick, including the ones it wrote nothing on.
//
// Every distance is in metres and every rate is per second. The defaults are
// documented against a metre-scaled world, which is the same assumption the two
// cell sizes are documented against — a cross-reference, not a coupling: the
// cell sizes tune the broadphase against typical Shape size while the Slop is a
// tolerance against world scale, so deriving one from the other would make
// retuning the broadphase silently change how deeply Bodies rest in each other.
//
// There is no per-Body override of any of them. cp has none, and none of the
// four solver settings is a property of a Body.
type Config struct {
	// Iterations is how many passes the impulse solver makes over the tick's
	// Contacts and Joints. Zero means the default, 10, which is cp's own.
	Iterations int

	// Slop is how far two Shapes may overlap and be left alone, in metres. Zero
	// means the default, 0.005 m.
	//
	// This is the one genuine conversion out of cp, whose 0.1 is pixel-sized
	// and in metres would allow a third of a 0.3 m radius as overlap. 0.005 m is
	// Box2D's linear slop in a metre world and 0.8% of a character on a 2 m
	// grid.
	Slop float64

	// Bias is how fast overlap is pushed out, as a rate per second. Zero means
	// the default, 6.32 /s.
	//
	// cp stores pow(0.9, 60) ≈ 0.001797 and computes 1 - pow(bias, dt), so its
	// stored number is already the share of overlap left after one second and
	// nothing needs converting — only re-expressing, because 0.0018 is
	// unreadable as a setting and Damping already fixed the spelling for this
	// shape of quantity. 1 − exp(−6.32/60) = 0.1 reproduces cp exactly.
	Bias float64

	// Persistence is how long a Contact that has stopped touching is kept
	// before it is dropped, in seconds. Zero means the default, 0.05 s.
	//
	// cp counts 3 ticks, which is 0.05 s at 60 Hz and 0.1 s at 30, and the
	// second is not what cp intends: it is a hysteresis window, not a frame
	// count. It is stored in seconds and converted to ticks internally.
	Persistence float64

	// StaticCellSize is the cell size of the static index, in metres. Zero
	// means the default, 2 m, which is cog's and has no counterpart in cp.
	StaticCellSize float64

	// BodyCellSize is the cell size of the Body index, in metres. Zero means
	// the default, 2 m.
	BodyCellSize float64

	// Seed seeds the one place randomness enters the solver: the nudge that
	// breaks the symmetry of two exactly coincident Shapes, in Detect. cp uses
	// a fixed (1, 0) there, which never breaks symmetry at all.
	//
	// Zero is an ordinary seed and not a request for an arbitrary one: nothing
	// here reaches for a clock, because a simulation that differs run to run for
	// a reason nobody asked for is worse than one that does not.
	Seed uint64
}
