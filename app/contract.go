// Package app declares the driver-agnostic application lifecycle and real-time
// loop contract. It contains no implementation and no windowing/GPU dependency.
// A driver plugin (e.g. wgpu) publishes these events and handles these commands,
// and gameplay/render plugins use them without importing any specific driver.
package app

// ViewportMode selects how the logical viewport responds to window aspect
// changes. ViewportWindow uses the window dimensions directly; fixed modes keep
// one dimension constant; Fit shows the full desired rectangle, while Cover
// fills the viewport from it.
type ViewportMode uint8

const (
	ViewportWindow ViewportMode = iota
	ViewportFixedWidth
	ViewportFixedHeight
	ViewportFit
	ViewportCover
)

// TimeAction selects what TimeCmd does to the engine's tick source: what
// decides when an update tick is published.
type TimeAction uint8

const (
	// TimeStatus reports the tick source without changing it.
	TimeStatus TimeAction = iota
	// TimePause stops update ticks, and stops nothing else. The driver keeps
	// drawing the last completed frame, input still reaches the input
	// contract, window size changes still publish, and the window stays live
	// and resizable — a paused game must not look hung.
	TimePause
	// TimeResume returns to the driver's frame clock from exactly where the
	// pause stopped. Nothing is banked while paused, however long it lasted,
	// so a resume costs no catch-up ticks.
	TimeResume
	// TimeStep publishes TimeRequest.Steps update ticks and implies TimePause:
	// stepping a running engine pauses it rather than being refused.
	TimeStep
)
