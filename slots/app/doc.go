// Package app declares the app Slot: the driver-agnostic application loop. It
// declares the events every loop publishes (InitEvent, UpdateEvent,
// RenderEvent, WindowSizeChangeEvent, QuitEvent), the QuitCmd and TimeCmd
// commands, and the Driver the Slot requires.
//
// The plugin, built by appplugin.New, owns everything about the loop that does
// not vary by platform: the fixed-step accumulator and render interpolation,
// the tick source behind TimeCmd with its pause, step and hold, tick numbering,
// and the publication of every event declared here. It offers the tick source
// to an agent as the tool app_time.
//
// app is a Slot: it requires exactly one Adapter for DriverPort, the platform
// main loop, which wgpu provides on the desktop and the web. app hands the
// Driver its Loop from Start and asks it to Quit; the Driver calls the Loop
// every frame. A composition without a Driver fails with
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
//	    wgpuplugin.New(), // provides app's Driver
//	    …
//	}
package app
