package internal

import "github.com/dvoyni/cog/slots/gfx"

// desiredViewport is the stored logical sizing policy behind
// SetDesiredViewportCmd. It stays private so only gfx resolves it; callers
// set it through the command and read the resolved Viewport resource.
type desiredViewport struct {
	mode          gfx.ViewportMode
	width, height float32
	size          float32
}
