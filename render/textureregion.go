package render

// TextureRegion is the rectangle of one mip level and one layer of a texture
// that WriteTexture writes: the level, the layer, its top-left texel, and its
// size in texels, a width or height of zero meaning the rest of the level
// from X or Y. The zero TextureRegion is the whole of mip level 0 of layer 0.
type TextureRegion struct {
	MipLevel, Layer uint32
	X, Y            uint32
	Width, Height   uint32
}
