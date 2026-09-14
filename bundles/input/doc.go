// Package input declares the driver-agnostic input Bundle: a unified Key space
// (keyboard keys AND mouse buttons), a polled State resource, discrete input
// events, and the Apply command a driver uses to feed input changes. Gameplay
// depends only on this package, never on a specific driver (e.g. gogpu).
//
// input is a Bundle. Its plugin, built by inputplugin.New, requires no Adapter
// and contributes one McpProvider.
//
// All state synchronization goes through the kernel's resource locks — there are
// no mutexes. A driver batches raw input into []Change and runs Apply; the Apply
// handler folds the batch into the State resource (under its write lock) and
// publishes the discrete events. A tick-boundary subscription on app.UpdateEvent,
// AdvanceOnUpdate, rolls the per-tick edges (JustPressed/JustReleased).
//
// The package also plays a scripted sequence of input into the engine through
// that same seam — Action, SynthesizeCmd and Play — carrying no mark that
// distinguishes it from a driver's own and no lifetime of its own. Tests,
// replays and demos are first-class callers; the two capabilities an agent
// reaches it through are an adapter over Play.
package input
