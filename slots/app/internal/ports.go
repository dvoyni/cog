package internal

import "github.com/dvoyni/cog/kernel"

// MainLoop is the interface app's required Adapter implements: the platform
// main loop, which runs the frames and calls app's Loop in each of them. A
// plugin that owns one (gogpu on the desktop and the web) fills MainLoopPort
// with registrar.ProvideAdapter during its Register, and is ordinarily the
// engine's Host as well.
//
// The two halves face opposite ways. app calls the MainLoop, and only to hand
// over its Loop and to stop the platform loop. The MainLoop calls the Loop,
// every frame, and reports through it everything else it does — measuring
// frame time, reading input, tracking the window.
type MainLoop interface {
	// Attach hands the MainLoop the Loop it calls. app calls it once, from its
	// Start, so it happens before the Host's Run and before any frame. The
	// MainLoop keeps the Loop for the engine's lifetime.
	Attach(loop Loop)
	// Quit stops the platform main loop, which unwinds the Host's Run and
	// shuts the engine down. app calls it for QuitCmd, from whatever goroutine
	// dispatched the command, so it must be safe to call from any goroutine.
	Quit()
	// ClipboardWrite puts text on the system clipboard. app calls it for
	// ClipboardWriteCmd, from whatever goroutine dispatched the command, so it
	// must be safe to call from any goroutine.
	ClipboardWrite(text string) error
}

// MainLoopPort is the Port app requires exactly one Adapter for: the platform
// main loop, which gogpu provides as gogpu.AppMainLoop. A composition without
// one fails with kernel.ErrMissingAdapter.
type MainLoopPort kernel.RequiredPort[MainLoop]
