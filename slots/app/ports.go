package app

import "github.com/dvoyni/cog/kernel"

// Driver is the interface app's Adapter implements: the platform main loop. A
// plugin that owns one (wgpu on the desktop and the web) fills DriverPort with
// registrar.ProvideAdapter during its Register, and is ordinarily the engine's
// Host as well.
//
// app hands the driver its Loop and asks it to stop, and nothing else.
// Everything the driver does besides — measuring frame time, reading input,
// tracking the window — it reports through that Loop.
type Driver interface {
	// Attach hands the driver the Loop it drives. app calls it once, from its
	// Start, so it happens before the Host's Run and before any frame. The
	// driver keeps the Loop for the engine's lifetime.
	Attach(loop Loop)
	// Quit stops the platform main loop, which unwinds the Host's Run and
	// shuts the engine down. app calls it for QuitCmd, from whatever goroutine
	// dispatched the command, so it must be safe to call from any goroutine.
	Quit()
}

// DriverPort is the Port app requires exactly one Adapter for: the platform
// main loop a driver such as wgpu provides. A composition without one fails
// with kernel.ErrMissingAdapter.
type DriverPort kernel.RequiredPort[Driver]
