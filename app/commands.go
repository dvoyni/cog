package app

import "github.com/dvoyni/cog/kernel"

// QuitCmd requests that the system driver stop its main loop.
type QuitCmd kernel.Command[QuitRequest, QuitResponse]

// QuitRequest is the empty request for QuitCmd.
type QuitRequest struct{}

// QuitResponse is the empty response from QuitCmd.
type QuitResponse struct{}

// TimeCmd controls the engine's tick source: it pauses the update loop,
// resumes it, steps it a named number of ticks, and reports which of those is
// true. Package app only declares it; the driver that owns the loop implements
// it, because only the host that owns the loop can stop it — and declaring it
// here is what lets gameplay code, a test harness or a frame-step debugger
// reach it without importing a driver.
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
	// are published together in one frame, and the driver's catch-up cap does
	// not apply to them: a step that dropped ticks would be a silent lie.
	// Each carries UpdateEvent.Last, so every step produces a complete frame.
	Steps int
	// Join asks a step to join a step already pending rather than raising
	// another. Something arming a per-tick observation under pause sets it, so
	// that arms landing together describe one tick instead of taking one tick
	// each. It is a tick-source behaviour before it is an agent-facing one.
	Join bool
}

// TimeResponse reports the tick source as this call left it. Every action
// answers with all of it, including TimeStatus.
type TimeResponse struct {
	// Paused reports whether update ticks are stopped.
	Paused bool
	// Changed reports whether this call changed the paused state. Pausing an
	// already-paused engine is an ordinary answer with Changed false, not an
	// error.
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
}

// SetViewportCmd updates the Viewport resource with the current render target
// size. A driver calls it when the size changes.
type SetViewportCmd kernel.Command[SetViewportRequest, SetViewportResponse]

// SetViewportRequest is the request for SetViewportCmd: the window size in
// device-independent pixels plus the physical framebuffer size.
type SetViewportRequest struct {
	Width, Height                       float32
	FramebufferWidth, FramebufferHeight float32
}

// SetViewportResponse reports the resolved logical and window dimensions.
type SetViewportResponse struct{ Viewport Viewport }

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd kernel.Command[SetDesiredViewportRequest, SetDesiredViewportResponse]

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest struct {
	Mode          ViewportMode
	Width, Height float32
	Size          float32
}

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse struct{ Viewport Viewport }
