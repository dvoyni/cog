// Package app declares the app Slot: the platform-agnostic application loop. It
// offers the events every loop publishes (InitEvent, UpdateEvent,
// RenderEvent, WindowSizeChangeEvent, QuitEvent), the QuitCmd and TimeCmd
// commands, and the MainLoop the Slot requires.
//
// The plugin, built by appplugin.New, owns everything about the loop that does
// not vary by platform: the fixed-step accumulator and render interpolation,
// the tick source behind TimeCmd with its pause, step and hold, tick numbering,
// and the publication of every event offered here. It offers the tick source
// to an agent as the tool app_time.
//
// app is a Slot: it requires exactly one Adapter for MainLoopPort, the platform
// main loop, which gogpu provides on the desktop and the web. app hands the
// MainLoop its Loop from Start and asks it to Quit; the MainLoop calls the Loop
// every frame. A composition without a MainLoop fails with
// kernel.ErrMissingAdapter.
//
// A plugin that dispatches QuitCmd or TimeCmd declares Name as a dependency,
// so the command always has its handler. Subscribing to app's events needs no
// dependency: an event without a publisher is simply never delivered.
//
//	config := map[kernel.PluginName]any{
//	    app.Name: app.Config{}.WithStep(time.Second / 30),
//	}
//	plugins := []kernel.Plugin{
//	    appplugin.New(),
//	    gogpuplugin.New(), // provides app's MainLoop
//	    …
//	}
package app
