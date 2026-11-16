package render

// ColorSpace is how a texture's file encodes its colours.
type ColorSpace uint8

const (
	// ColorSpaceSRGB is colour encoded in sRGB, as most pictures are: the
	// texture is in an sRGB format, so sampling it answers linear colour.
	ColorSpaceSRGB ColorSpace = iota
	// ColorSpaceLinear is data or linear colour, such as a normal map or a
	// mask: the texture answers its values as they are in the file.
	ColorSpaceLinear
)

// makeTextureFormat answers the format of a texture whose file is in s, of 8
// bits per channel.
func (s ColorSpace) makeTextureFormat() TextureFormat {
	if s == ColorSpaceLinear {
		return TextureFormatRGBA8Unorm
	}
	return TextureFormatRGBA8UnormSrgb
}
