package gfx

import "github.com/dvoyni/cog/app"

// desiredViewport is the stored logical sizing policy behind
// app.SetDesiredViewportCmd. It stays private so only gfx resolves it; callers
// set it through the command and read the resolved app.Viewport resource.
type desiredViewport struct {
	mode          app.ViewportMode
	width, height float32
	size          float32
}
