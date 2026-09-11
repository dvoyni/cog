package wgpu

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// The driver offers its one capability through the agent-facing extension
// point. It is the provider because mcp.Provider embeds kernel.Plugin and app
// has no plugin at all: only the host that owns the loop can stop it, so a
// different host would offer its own sdl_time with its own answer.
var _ mcp.Provider = (*Plugin)(nil)

// timeName is the driver's one capability, rendered as the tool wgpu_time.
const timeName = "time"

// maxTimeSteps caps one step request at ten seconds of simulation. A request
// that outlives its deadline leaves ticks the capability body can no longer
// unwind, so the window in which one can be created and then orphaned is
// bounded — the same figure, for the same reason, as the other capped spans.
const maxTimeSteps = 600

// stepDeadline is this capability's own wait for its ticks, well below the
// broker's own timeout so the failure names the likely cause instead of
// reporting a generic engine stall. A step lands on the next frame, so seconds
// are already generous; what this really bounds is a window that has stopped
// producing frames at all.
const stepDeadline = 5 * time.Second

// timeDescription is prompt text, and it is reproduced in
// wgpu/docs/specs/mcp.md so it is reviewed as prompt text rather than buried
// as a string literal.
const timeDescription = "Stop, start or single-step the game's update loop. `pause` stops update " +
	"ticks; the window keeps drawing the last completed frame, stays responsive and can still be " +
	"captured, so a paused game does not look hung. `step` advances exactly the number of ticks " +
	"you ask for and implies pause. `resume` returns to real time from exactly where it stopped — " +
	"no time is banked and nothing catches up. `status` just reports, and every answer names the " +
	"current `tick`.\n\n" +
	"Use this to take an observation that nothing moved underneath. Paused, two captures are " +
	"identical, and `key_down`, `step 1`, `key_up`, `step 1` holds a key for exactly one tick — " +
	"something a running engine cannot do. Snapshots (`canvas_draws`, `ui_layout`, `gfx_frame`) " +
	"each need a tick, so while paused they perform one step themselves and say so.\n\n" +
	"To make several snapshots describe *one* tick, call `hold` first, in the same batch of " +
	"parallel calls as the snapshots and listed before them. A hold keeps the step they share " +
	"open until you `release` it or until `ms` runs out (default 1000, maximum 10000), instead " +
	"of letting the next drawn frame close it — without one, whether the calls pair depends on " +
	"whether they all arrive inside the same ~16 ms gap, and they often do not. Then check it: " +
	"every snapshot reports the `tick` it describes, and they paired only if that number is the " +
	"same in all of them. Take `gfx_capture` last, after the snapshots have answered, because " +
	"it costs no tick and so shows whatever the shared step produced. If a hold runs out before " +
	"you release it, the next answer here says `holdExpired`.\n\n" +
	"This stops cog's tick, and only that. Animation driven by ticks freezes; anything a game " +
	"times by its own wall-clock does not. There is no slow motion. Nothing resumes the game when " +
	"you disconnect — it stays paused until something resumes it, and a hold you walk away from " +
	"expires by itself."

// TimeRequest asks the engine to pause, resume, step, hold, release or report.
type TimeRequest struct {
	Action string `json:"action" jsonschema:"one of pause, resume, step, hold, release or status"`
	Steps  int    `json:"steps,omitempty" jsonschema:"how many ticks step advances; default 1, maximum 600"`
	Ms     int    `json:"ms,omitempty" jsonschema:"hold: how long the hold may stand before expiring, in milliseconds; default 1000, maximum 10000"`
}

// TimeResponse reports the tick source as the call left it, on every action.
type TimeResponse struct {
	// Paused reports whether update ticks are stopped.
	Paused bool `json:"paused"`
	// Stepped is how many ticks this call advanced the game by.
	Stepped int `json:"stepped"`
	// Advanced is how many ticks have been stepped since the pause began.
	Advanced int `json:"advanced"`
	// Tick is the number of the last tick published, which is the number a
	// snapshot of that tick reports. It is what an agent correlates a
	// snapshot against, and unlike Advanced it never resets.
	Tick int64 `json:"tick"`
	// Held reports whether a hold is keeping the step window open, and HoldMs
	// how much longer it may.
	Held   bool `json:"held"`
	HoldMs int  `json:"holdMs,omitempty"`
	// HoldExpired reports that the last hold ran out instead of being
	// released, which is the one way a pairing can still split. It stays set
	// until the next hold begins.
	HoldExpired bool `json:"holdExpired,omitempty"`
}

// Capabilities reports what the driver offers an agent: control of the tick
// source, and nothing else. It is an engine feature the extension point
// happens to want, so the contract is app.TimeCmd and a test harness or a
// frame-step debugger reaches it on the same terms.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{mcp.Func(timeName, timeDescription, timeControl)}
}

// timeControl is the wgpu_time body. It is a Func rather than a Command
// because a step waits for a frame and so carries its own deadline, and
// because the action is validated before anything is armed: a bad request
// costs no frames.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site — the driver is one pointer away and the body
// still reaches it only by dispatch.
func timeControl(k kernel.Executioner, request TimeRequest) (TimeResponse, error) {
	action, known := timeActions[request.Action]
	if !known {
		return TimeResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"%q is not an action — use pause, resume, step, hold, release or status", request.Action)}
	}
	if action != app.TimeStep && request.Steps != 0 {
		return TimeResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"steps applies to step, not to %s", request.Action)}
	}
	if request.Steps < 0 || request.Steps > maxTimeSteps {
		return TimeResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"steps is %d; ask for between 1 and %d — ten seconds of simulation — and call step "+
				"again for more", request.Steps, maxTimeSteps)}
	}
	if action != app.TimeHold && request.Ms != 0 {
		return TimeResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"ms applies to hold, not to %s", request.Action)}
	}
	if request.Ms < 0 || time.Duration(request.Ms)*time.Millisecond > maxHoldDuration {
		return TimeResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"ms is %d; hold for between 1 and %d — a window an agent that walks away cannot "+
				"leave open — and hold again for another", request.Ms,
			maxHoldDuration.Milliseconds())}
	}

	// A step waits for a frame, and a hold is somebody's deliberate decision
	// to postpone one, so the wait that names a stalled engine is extended by
	// however long the window may stay open. Nothing else here waits at all.
	if action == app.TimeStep {
		ctx, cancel := context.WithTimeout(k.Context(), stepDeadline+app.HoldRemaining(k))
		defer cancel()
		k = k.WithContext(ctx)
	}
	answer, err := k.ExecuteCommand[app.TimeCmd](app.TimeRequest{
		Action: action, Steps: request.Steps,
		Hold: time.Duration(request.Ms) * time.Millisecond,
	})
	if err != nil {
		return TimeResponse{}, timeFailure(err)
	}
	if refusal, refused := timeRefusal(action, answer); refused {
		return TimeResponse{}, refusal
	}
	return TimeResponse{
		Paused:      answer.Paused,
		Stepped:     answer.Stepped,
		Advanced:    answer.Advanced,
		Tick:        answer.Tick,
		Held:        answer.Held,
		HoldMs:      int(answer.HoldFor.Milliseconds()),
		HoldExpired: answer.HoldExpired,
	}, nil
}

// timeFailure turns a dispatch failure into words an agent reads and acts on.
// The domain refusals the tick source raises are expected outcomes rather
// than faults, so they read as prose here; anything else travels as it is.
func timeFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"no tick was published within %s — the window may be minimised, the game may have "+
				"stopped rendering, or a hold may still be open; the steps will run when it "+
				"draws again", stepDeadline)}
	}
	var tooLong ErrHoldTooLong
	if errors.As(err, &tooLong) {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"a hold may stand for at most %d ms, and %d was asked for",
			tooLong.Max.Milliseconds(), tooLong.For.Milliseconds())}
	}
	return err
}

// timeActions maps the wire's action onto the contract's. The mapping lives
// here rather than on app.TimeAction because an engine contract owes nothing
// to a wire format.
var timeActions = map[string]app.TimeAction{
	"status":  app.TimeStatus,
	"pause":   app.TimePause,
	"resume":  app.TimeResume,
	"step":    app.TimeStep,
	"hold":    app.TimeHold,
	"release": app.TimeRelease,
}

// timeRefusal turns a request that asked for a state the engine is already in
// into an expected outcome the agent reads and moves past, rather than an
// error. The reason carries the state, because an Unavailable is words rather
// than the response struct every other call answers with.
func timeRefusal(action app.TimeAction, answer app.TimeResponse) (mcp.Unavailable, bool) {
	if answer.Changed {
		return mcp.Unavailable{}, false
	}
	switch action {
	case app.TimePause:
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the engine is already paused, %d ticks in — step to advance it, or resume to let "+
				"it run", answer.Advanced)}, true
	case app.TimeResume:
		return mcp.Unavailable{Reason: "the engine is not paused — it is already running in real time"}, true
	case app.TimeHold:
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"a hold is already open, for another %d ms — arm what you want paired, then release "+
				"it", answer.HoldFor.Milliseconds())}, true
	case app.TimeRelease:
		if answer.HoldExpired {
			return mcp.Unavailable{Reason: "the hold ran out before you released it, so anything " +
				"armed after it ran out is on a later tick — compare the tick each snapshot " +
				"reports, and hold again with a longer ms if they differ"}, true
		}
		return mcp.Unavailable{Reason: "no hold is open — hold first, then arm what you want " +
			"paired, then release"}, true
	default:
		return mcp.Unavailable{}, false
	}
}
