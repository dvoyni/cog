package gfx

// Viewport holds the logical world size, device-independent window size, and
// physical framebuffer size. A game chooses the logical sizing policy through
// SetDesiredViewportCmd; the driver supplies both output sizes through
// SetViewportCmd.
type Viewport struct {
	Width, Height                       float32
	WindowWidth, WindowHeight           float32
	FramebufferWidth, FramebufferHeight float32
}
