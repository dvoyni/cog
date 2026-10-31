package app

import "github.com/dvoyni/cog/slots/app/internal"

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
type MainLoop = internal.MainLoop

// MainLoopPort is the Port app requires exactly one Adapter for: the platform
// main loop, which gogpu provides as gogpu.AppMainLoop. A composition without
// one fails with kernel.ErrMissingAdapter.
type MainLoopPort = internal.MainLoopPort
