package internal

import "github.com/dvoyni/cog/slots/gfx/internal/types"

// desiredViewport is the stored logical sizing policy behind
// SetDesiredViewportCmd. It stays private so only gfx resolves it; callers
// set it through the command and read the resolved Viewport resource.
type desiredViewport struct {
	mode          types.ViewportMode
	width, height float32
	size          float32
}
