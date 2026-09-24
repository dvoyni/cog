package app

import "github.com/dvoyni/cog/slots/app/internal"

// QuitCmd requests that the application stop: app asks its MainLoop to quit the
// platform main loop, which unwinds the Host's Run and shuts the engine down.
// It returns once the request is made, not once the loop has stopped.
type QuitCmd = internal.QuitCmd

// QuitRequest is the empty request for QuitCmd.
type QuitRequest = internal.QuitRequest

// QuitResponse is the empty response from QuitCmd.
type QuitResponse = internal.QuitResponse

// ClipboardWriteCmd puts text on the system clipboard through the MainLoop.
// Reading the clipboard is not a command: a browser hands pasted text only to
// the paste itself, so pasted text arrives as input (input.ClipboardPasteEvent
// and input.State.ClipboardPaste) wherever it can arrive at all.
type ClipboardWriteCmd = internal.ClipboardWriteCmd

// ClipboardWriteRequest carries the text to put on the clipboard.
type ClipboardWriteRequest = internal.ClipboardWriteRequest

// ClipboardWriteResponse carries the failure when the platform refused the
// write.
type ClipboardWriteResponse = internal.ClipboardWriteResponse

// TimeCmd controls the engine's tick source: it pauses the update loop,
// resumes it, steps it a named number of ticks, and reports which of those is
// true. The app plugin handles it, because the tick source is part of the loop
// app owns, and a plugin that declares Name as a dependency is guaranteed that
// handler — which is what lets gameplay code, a test harness or a frame-step
// debugger reach it without importing a platform plugin, and without reading a missing
// handler as a running engine.
//
// This is an engine feature rather than a debugging aside, and it comes with
// two limits stated as non-guarantees rather than left to be discovered:
//
// cog can stop the tick; it cannot slow it. UpdateEvent.Dt is a constant and
// there is no engine clock to distort, so there is no time scale and there
// should not be one.
//
// A game that reads wall-clock time itself is outside this contract, and pause
// cannot reach it. Animation driven by ticks freezes; animation a game times
// with its own time.Now does not.
//
// TimeStep does not return until its ticks have been published, so a caller
// that wants the tick to have happened simply waits for the dispatch. The
// caller's context bounds that wait; steps already requested when it expires
// are still published.
type TimeCmd = internal.TimeCmd

// TimeRequest selects the action and, for TimeStep, how many ticks.
type TimeRequest = internal.TimeRequest

// TimeResponse reports the tick source as this call left it. Every action
// answers with all of it, including TimeStatus.
type TimeResponse = internal.TimeResponse
