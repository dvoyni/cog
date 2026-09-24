package internal

// desiredViewport is the stored logical sizing policy behind
// SetDesiredViewportCmd. It stays private so only gfx resolves it; callers
// set it through the command and read the resolved Viewport resource.
type desiredViewport struct {
	mode          ViewportMode
	width, height float32
	size          float32
}

// Viewport holds the logical world size, device-independent window size, and
// physical framebuffer size. A game chooses the logical sizing policy through
// SetDesiredViewportCmd; the driver supplies both output sizes through
// SetViewportCmd.
type Viewport struct {
	Width, Height                       float32
	WindowWidth, WindowHeight           float32
	FramebufferWidth, FramebufferHeight float32
}
