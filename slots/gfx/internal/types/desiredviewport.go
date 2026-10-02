package types

// DesiredViewport is the stored logical sizing policy behind
// SetDesiredViewportCmd. The root package does not re-export it, so only gfx
// resolves it; callers set it through the command and read the resolved
// Viewport resource.
type DesiredViewport struct {
	Mode          ViewportMode
	Width, Height float32
	Size          float32
}
