package render

// SurfaceRequest is what Surface.Request asks every window's surface to be
// configured with, from the next configure on. A part a surface cannot
// honour falls back, with an error logged: the format to
// TextureFormatBGRA8Unorm and the present mode to PresentModeFifo.
type SurfaceRequest struct {
	// Format is the framebuffer's format; zero is TextureFormatBGRA8Unorm,
	// which every backend accepts, leaving sRGB encoding to the last pass.
	// TextureFormatBGRA8UnormSrgb is honoured where the backend's surfaces
	// offer it; gogpu's offer BGRA8Unorm alone.
	Format TextureFormat
	// PresentMode is the present mode; zero is PresentModeFifo. gogpu's
	// surfaces offer Fifo alone.
	PresentMode PresentMode
}
