package app

import "github.com/dvoyni/cog/kernel"

// Loop is the platform-neutral half of the application loop: what a Driver
// calls, and app implements. app hands it over through Driver.Attach. Each
// method publishes one of app's events and waits for its subscribers, so the
// ordering a driver chooses between its own work and these calls is the
// ordering subscribers observe.
//
// Every method takes the Executioner the driver holds, the Host's from Run,
// because the calls come from driver callbacks outside any handler.
type Loop interface {
	// Init publishes InitEvent. The driver calls it once, on the thread that
	// will run its loop, immediately before entering that loop; a non-nil error
	// means the loop must not start.
	Init(k kernel.Executioner) error
	// Frame runs one frame of the fixed-step update on the driver's main
	// thread. It folds dt, the real seconds the frames rendered since the last
	// call took, into the accumulator and publishes the UpdateEvents it owes,
	// numbered and in order. While the tick source is paused dt is discarded
	// and only requested steps publish. A driver flushes the frame's input
	// before calling it, so every tick of the frame sees that input.
	Frame(k kernel.Executioner, dt float64)
	// Render publishes RenderEvent for one drawn frame, carrying the
	// interpolation factor the last Frame left. The driver calls it on its
	// render thread once the frame's target is current; it may run while Frame
	// runs on the main thread.
	Render(k kernel.Executioner)
	// WindowSize publishes WindowSizeChangeEvent. The driver calls it when the
	// window's size in device-independent pixels changes, before it resolves
	// that frame's viewport.
	WindowSize(k kernel.Executioner, width, height float32)
	// Quit publishes QuitEvent. The driver calls it once, after its loop
	// returns.
	Quit(k kernel.Executioner)
}

// TimeAction selects what TimeCmd does to the engine's tick source: what
// decides when an update tick is published.
type TimeAction uint8

const (
	// TimeStatus reports the tick source without changing it.
	TimeStatus TimeAction = iota
	// TimePause stops update ticks, and stops nothing else. The driver keeps
	// drawing the last completed frame, input still reaches the input
	// plugin, window size changes still publish, and the window stays live
	// and resizable — a paused game must not look hung.
	TimePause
	// TimeResume returns to the driver's frame clock from exactly where the
	// pause stopped. Nothing is banked while paused, however long it lasted,
	// so a resume costs no catch-up ticks.
	TimeResume
	// TimeStep publishes TimeRequest.Steps update ticks and implies TimePause:
	// stepping a running engine pauses it rather than being refused.
	TimeStep
	// TimeHold keeps the step window open until TimeRelease or until
	// TimeRequest.Hold runs out, so that steps requested over several frames
	// still publish as one tick. It implies TimePause for the reason TimeStep
	// does: holding a running engine is meaningless.
	//
	// Without it the window is only as wide as the gap before the next
	// rendered frame, and whether two requests share a tick depends on
	// whether they both fit inside it.
	TimeHold
	// TimeRelease ends a hold early. The step the hold was keeping open
	// publishes on the next frame, as it would have without the hold.
	TimeRelease
)
