package app

import (
	"time"

	"github.com/dvoyni/cog/kernel"
)

// QuitCmd requests that the application stop: app asks its Driver to quit the
// platform main loop, which unwinds the Host's Run and shuts the engine down.
// It returns once the request is made, not once the loop has stopped.
type QuitCmd kernel.Command[QuitRequest, QuitResponse]

// QuitRequest is the empty request for QuitCmd.
type QuitRequest struct{}

// QuitResponse is the empty response from QuitCmd.
type QuitResponse struct{}

// TimeCmd controls the engine's tick source: it pauses the update loop,
// resumes it, steps it a named number of ticks, and reports which of those is
// true. The app plugin handles it, because the tick source is part of the loop
// app owns, and a plugin that declares Name as a dependency is guaranteed that
// handler — which is what lets gameplay code, a test harness or a frame-step
// debugger reach it without importing a driver, and without reading a missing
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
type TimeCmd kernel.Command[TimeRequest, TimeResponse]

// TimeRequest selects the action and, for TimeStep, how many ticks.
type TimeRequest struct {
	// Action is what to do to the tick source.
	Action TimeAction
	// Steps is how many update ticks TimeStep publishes; zero means one. They
	// are published together in one frame, and the loop's catch-up cap does
	// not apply to them: a step that dropped ticks would be a silent lie.
	// Each carries UpdateEvent.Last, so every step produces a complete frame.
	Steps int
	// Join asks a step to join a step already pending rather than raising
	// another. Something arming a per-tick observation under pause sets it, so
	// that arms landing together describe one tick instead of taking one tick
	// each. It is a tick-source behaviour before it is an agent-facing one.
	//
	// Joining is opportunistic on its own: it can only join a step that is
	// still pending, and a rendered frame publishes the pending step as soon
	// as it finds one. TimeHold is what makes it deterministic.
	Join bool
	// Hold is how long a TimeHold may stand before it expires by itself; zero
	// asks for the default, one second. app caps it at ten seconds, and
	// refuses anything above the cap with ErrHoldTooLong rather than silently
	// shortening it: a hold nobody ends is an engine nothing can step.
	Hold time.Duration
}

// TimeResponse reports the tick source as this call left it. Every action
// answers with all of it, including TimeStatus.
type TimeResponse struct {
	// Paused reports whether update ticks are stopped.
	Paused bool
	// Changed reports whether this call changed the state it asked to change:
	// the paused state for TimePause, TimeResume and TimeStep, the hold for
	// TimeHold and TimeRelease. Pausing an already-paused engine is an
	// ordinary answer with Changed false, not an error.
	Changed bool
	// Stepped is how many ticks the step this call waited for published. Calls
	// sharing one step all report that step's ticks, which is the point of
	// sharing it.
	Stepped int
	// Joined reports that the step joined one already pending rather than
	// raising its own.
	Joined bool
	// Advanced is how many ticks have been stepped since the pause began. It
	// resets when a pause begins, so after a resume it reports the total for
	// the pause that just ended.
	Advanced int
	// Tick is the number of the last update tick published, matching
	// UpdateEvent.Tick. It is what correlates a time-control answer with
	// whatever else describes a tick; it never resets.
	Tick int64
	// Held reports whether a hold currently stands, and HoldFor how much
	// longer it may stand before expiring. A caller about to wait for a step
	// adds HoldFor to its own deadline, so that a window somebody deliberately
	// held open is not read as a stalled engine.
	Held    bool
	HoldFor time.Duration
	// HoldExpired reports that the most recent hold ended on its deadline
	// rather than being released. It stays true until the next hold begins,
	// because the caller that needs to know is the one that comes back to a
	// window it thought it still had.
	HoldExpired bool
}
