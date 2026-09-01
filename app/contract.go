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
