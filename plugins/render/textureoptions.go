package render

// TextureOptions are the load options that make one file several textures:
// each set of them is its own loaded texture.
type TextureOptions struct {
	// ColorSpace is how the file encodes its colours.
	ColorSpace ColorSpace
	// Mipmaps gives the texture every mip level down to one texel, each
	// drawn from the level above it in the frame its body is written.
	Mipmaps bool
}
