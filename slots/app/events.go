package app

import "github.com/dvoyni/cog/slots/app/internal"

// InitEvent is published once, immediately before the MainLoop enters its main
// loop.
type InitEvent = internal.InitEvent

// UpdateEvent is the fixed-timestep simulation tick event. app publishes it
// once per fixed step, in order, on the MainLoop's main thread, catching up when
// frames run long.
type UpdateEvent = internal.UpdateEvent

// RenderEvent is the per-frame render event. app publishes it once per drawn
// frame, on the MainLoop's render thread.
type RenderEvent = internal.RenderEvent

// PauseChangeEvent reports that the tick source started or stopped publishing
// update ticks. app publishes it from Frame, on the MainLoop's main thread, on
// the first frame that observes the change and before that frame's ticks - so a
// subscriber has already acted on the pause by the time a stepped tick reaches
// it, and has already acted on the resume by the time the frame clock's first
// tick does.
//
// It exists because pause stops app.UpdateEvent and an engine whose only signal
// is the absence of an event cannot act on one. A Slot that must do something
// at the moment of a pause - sound suspends every Voice, because a paused game
// that keeps playing footsteps is a bug in every game that hits it - has
// nothing to subscribe to otherwise, and polling TimeCmd every drawn frame to
// ask a question whose answer almost never changes is the shape the tick
// source's own atomics exist to avoid.
//
// It reports the change and not the state: a subscriber is told when pause
// begins and when it ends, never once per frame that it stands.
type PauseChangeEvent = internal.PauseChangeEvent

// QuitEvent is published once, after the MainLoop's platform loop returns.
type QuitEvent = internal.QuitEvent

// WindowSizeChangeEvent reports a change to the window size in device-independent
// pixels. app publishes it when the MainLoop reports one, before the MainLoop
// resolves the logical viewport.
type WindowSizeChangeEvent = internal.WindowSizeChangeEvent
