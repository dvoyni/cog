package render

import "github.com/dvoyni/cog/plugins/gfx"

// defaultTextureKey is which default texture a default texture view is of:
// the view's kind, and the texture's dimension.
type defaultTextureKey struct {
	defaultTextureKind
	dimension gfx.TextureDimension
}

// makeDescriptor answers the descriptor of the default texture k names, of
// one texel, with six layers in 2D for every view of 2D, arrays and cubes,
// and four samples when multisampled; and the bytes of the texel it is
// filled with: white, at depth 1.0, or none, for zeros, for integers, a
// storage format or several samples.
func (k defaultTextureKey) makeDescriptor() (gfx.TextureDescriptor, []byte) {
	descriptor := gfx.TextureDescriptor{
		Label:         defaultLabel + " texture",
		Size:          gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     k.dimension,
		Usage:         gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagCopyDst,
	}
	if k.dimension == gfx.TextureDimension2D && !k.multisampled {
		descriptor.Size.DepthOrArrayLayers = 6
	}
	var texel []byte
	switch k.sampleType {
	case gfx.TextureSampleTypeFloat:
		descriptor.Format = gfx.TextureFormatRGBA8Unorm
		texel = []byte{0xff, 0xff, 0xff, 0xff}
	case gfx.TextureSampleTypeDepth:
		descriptor.Format = gfx.TextureFormatDepth16Unorm
		texel = []byte{0xff, 0xff}
	case gfx.TextureSampleTypeSint:
		descriptor.Format = gfx.TextureFormatRGBA8Sint
	case gfx.TextureSampleTypeUint:
		descriptor.Format = gfx.TextureFormatRGBA8Uint
	default:
		descriptor.Format = k.storageFormat
		descriptor.Usage = gfx.TextureUsageFlagStorageBinding
	}
	if k.multisampled {
		descriptor.SampleCount = multisampledDefaultSampleCount
		descriptor.Usage = gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagRenderAttachment
		texel = nil
	}
	return descriptor, texel
}
