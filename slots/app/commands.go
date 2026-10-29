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

// CursorCmd shows or hides the mouse cursor over the window, through the
// MainLoop: a game hides it while it is played with something other than the
// mouse, and shows it again when the mouse is taken up. It returns once the
// wish is recorded; the platform applies it on its next frame.
type CursorCmd = internal.CursorCmd

// CursorRequest carries whether the cursor is hidden; the zero request shows
// it.
type CursorRequest = internal.CursorRequest

// CursorResponse is the empty response from CursorCmd.
type CursorResponse = internal.CursorResponse

// FullscreenCmd puts the window into fullscreen or back into a window, through
// the MainLoop. It returns once the wish is recorded; the platform applies it
// on its next frame. A browser enters fullscreen only in answer to a user's
// gesture, so there it is asked for straight after a click or not at all.
type FullscreenCmd = internal.FullscreenCmd

// FullscreenRequest carries whether the window is fullscreen; the zero request
// makes it a window.
type FullscreenRequest = internal.FullscreenRequest

// FullscreenResponse is the empty response from FullscreenCmd.
type FullscreenResponse = internal.FullscreenResponse

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
