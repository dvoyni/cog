// Package internal is the app plugin: New, the Loop a MainLoop drives, the tick
// source behind app.TimeCmd, the QuitCmd and TimeCmd handlers, and its mcp
// provider. Composition roots and tests reach New through appplugin.
//
// It owns the platform-neutral half of the application loop: the fixed-step
// accumulator and render interpolation, pause, step and hold, tick numbering,
// and the publication of every app event. The platform half — the OS main
// loop, measuring real frame time, input and the window — is the app.MainLoop
// Adapter it requires exactly one of, and hands its Loop to from Start.
package internal
