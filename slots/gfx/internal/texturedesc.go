package internal

import "github.com/dvoyni/cog/slots/gfx/internal/types"

// TextureDesc describes a texture to create. Layers <= 1 creates a regular 2D
// texture; larger values create a 2D-array texture. Renderable asks for a
// texture a render pass can draw into as well as sample.
type TextureDesc struct {
	Width, Height int
	Layers        int
	Format        types.TextureFormat
	Mipmaps       bool
	Renderable    bool
	Label         string
}
