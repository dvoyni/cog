package app

import "github.com/dvoyni/cog/slots/app/internal"

// Loop is the platform-neutral half of the application loop: what app
// implements and the MainLoop calls, every frame, from inside the platform
// loop it runs. It is the opposite direction to MainLoop, which is what app
// calls; app hands the Loop over through MainLoop.Attach. Each method
// publishes one of app's events and waits for its subscribers, so the ordering
// a MainLoop chooses between its own work and these calls is the ordering
// subscribers observe.
//
// Every method takes the Executioner the MainLoop holds, the Host's from Run,
// because the calls come from platform callbacks outside any handler.
type Loop = internal.Loop

// TimeAction selects what TimeCmd does to the engine's tick source: what
// decides when an update tick is published.
type TimeAction = internal.TimeAction

const (
	// TimeStatus reports the tick source without changing it.
	TimeStatus = internal.TimeStatus
	// TimePause stops update ticks, and stops nothing else. The MainLoop keeps
	// drawing the last completed frame, input still reaches the input
	// plugin, window size changes still publish, and the window stays live
	// and resizable — a paused game must not look hung.
	TimePause = internal.TimePause
	// TimeResume returns to the MainLoop's frame clock from exactly where the
	// pause stopped. Nothing is banked while paused, however long it lasted,
	// so a resume costs no catch-up ticks.
	TimeResume = internal.TimeResume
	// TimeStep publishes TimeRequest.Steps update ticks and implies TimePause:
	// stepping a running engine pauses it rather than being refused.
	TimeStep = internal.TimeStep
	// TimeHold keeps the step window open until TimeRelease or until
	// TimeRequest.Hold runs out, so that steps requested over several frames
	// still publish as one tick. It implies TimePause for the reason TimeStep
	// does: holding a running engine is meaningless.
	//
	// Without it the window is only as wide as the gap before the next
	// rendered frame, and whether two requests share a tick depends on
	// whether they both fit inside it.
	TimeHold = internal.TimeHold
	// TimeRelease ends a hold early. The step the hold was keeping open
	// publishes on the next frame, as it would have without the hold.
	TimeRelease = internal.TimeRelease
)
