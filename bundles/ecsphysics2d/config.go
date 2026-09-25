package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal"

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
// whole frame, on every tick, including the ones it wrote nothing on. The
// values that are a property of the scene rather than of the solver, gravity
// among them, are Constants instead, and an app that changes them pays that.
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
type Config = internal.Config
