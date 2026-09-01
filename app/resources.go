package app

// Viewport holds the logical world size, device-independent window size, and
// physical framebuffer size. A game chooses the logical sizing policy through
// SetDesiredViewportCmd; the driver supplies both output sizes through
// SetViewportCmd.
//
// This package is a contract only, so the type is declared here rather than
// aliased: a renderer plugin owns the resource and handles the commands.
type Viewport struct {
	Width, Height                       float32
	WindowWidth, WindowHeight           float32
	FramebufferWidth, FramebufferHeight float32
}
