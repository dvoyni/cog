package render

import "github.com/dvoyni/cog/gfx"

// TextureFormat is the layout of a texture's texels, each format gfx's of
// the same name. A temporary texture carries every usage its format
// allows: it is sampled and copied, drawn into when the format is colour
// renderable or depth-stencil, and bound for storage when the format allows
// it on WebGPU's core and the texture has one sample.
type TextureFormat uint32

const (
	TextureFormatUndefined            = TextureFormat(gfx.TextureFormatUndefined)
	TextureFormatR8Unorm              = TextureFormat(gfx.TextureFormatR8Unorm)
	TextureFormatR8Snorm              = TextureFormat(gfx.TextureFormatR8Snorm)
	TextureFormatR8Uint               = TextureFormat(gfx.TextureFormatR8Uint)
	TextureFormatR8Sint               = TextureFormat(gfx.TextureFormatR8Sint)
	TextureFormatR16Unorm             = TextureFormat(gfx.TextureFormatR16Unorm)
	TextureFormatR16Snorm             = TextureFormat(gfx.TextureFormatR16Snorm)
	TextureFormatR16Uint              = TextureFormat(gfx.TextureFormatR16Uint)
	TextureFormatR16Sint              = TextureFormat(gfx.TextureFormatR16Sint)
	TextureFormatR16Float             = TextureFormat(gfx.TextureFormatR16Float)
	TextureFormatRG8Unorm             = TextureFormat(gfx.TextureFormatRG8Unorm)
	TextureFormatRG8Snorm             = TextureFormat(gfx.TextureFormatRG8Snorm)
	TextureFormatRG8Uint              = TextureFormat(gfx.TextureFormatRG8Uint)
	TextureFormatRG8Sint              = TextureFormat(gfx.TextureFormatRG8Sint)
	TextureFormatR32Float             = TextureFormat(gfx.TextureFormatR32Float)
	TextureFormatR32Uint              = TextureFormat(gfx.TextureFormatR32Uint)
	TextureFormatR32Sint              = TextureFormat(gfx.TextureFormatR32Sint)
	TextureFormatRG16Unorm            = TextureFormat(gfx.TextureFormatRG16Unorm)
	TextureFormatRG16Snorm            = TextureFormat(gfx.TextureFormatRG16Snorm)
	TextureFormatRG16Uint             = TextureFormat(gfx.TextureFormatRG16Uint)
	TextureFormatRG16Sint             = TextureFormat(gfx.TextureFormatRG16Sint)
	TextureFormatRG16Float            = TextureFormat(gfx.TextureFormatRG16Float)
	TextureFormatRGBA8Unorm           = TextureFormat(gfx.TextureFormatRGBA8Unorm)
	TextureFormatRGBA8UnormSrgb       = TextureFormat(gfx.TextureFormatRGBA8UnormSrgb)
	TextureFormatRGBA8Snorm           = TextureFormat(gfx.TextureFormatRGBA8Snorm)
	TextureFormatRGBA8Uint            = TextureFormat(gfx.TextureFormatRGBA8Uint)
	TextureFormatRGBA8Sint            = TextureFormat(gfx.TextureFormatRGBA8Sint)
	TextureFormatBGRA8Unorm           = TextureFormat(gfx.TextureFormatBGRA8Unorm)
	TextureFormatBGRA8UnormSrgb       = TextureFormat(gfx.TextureFormatBGRA8UnormSrgb)
	TextureFormatRGB10A2Uint          = TextureFormat(gfx.TextureFormatRGB10A2Uint)
	TextureFormatRGB10A2Unorm         = TextureFormat(gfx.TextureFormatRGB10A2Unorm)
	TextureFormatRG11B10Ufloat        = TextureFormat(gfx.TextureFormatRG11B10Ufloat)
	TextureFormatRGB9E5Ufloat         = TextureFormat(gfx.TextureFormatRGB9E5Ufloat)
	TextureFormatRG32Float            = TextureFormat(gfx.TextureFormatRG32Float)
	TextureFormatRG32Uint             = TextureFormat(gfx.TextureFormatRG32Uint)
	TextureFormatRG32Sint             = TextureFormat(gfx.TextureFormatRG32Sint)
	TextureFormatRGBA16Unorm          = TextureFormat(gfx.TextureFormatRGBA16Unorm)
	TextureFormatRGBA16Snorm          = TextureFormat(gfx.TextureFormatRGBA16Snorm)
	TextureFormatRGBA16Uint           = TextureFormat(gfx.TextureFormatRGBA16Uint)
	TextureFormatRGBA16Sint           = TextureFormat(gfx.TextureFormatRGBA16Sint)
	TextureFormatRGBA16Float          = TextureFormat(gfx.TextureFormatRGBA16Float)
	TextureFormatRGBA32Float          = TextureFormat(gfx.TextureFormatRGBA32Float)
	TextureFormatRGBA32Uint           = TextureFormat(gfx.TextureFormatRGBA32Uint)
	TextureFormatRGBA32Sint           = TextureFormat(gfx.TextureFormatRGBA32Sint)
	TextureFormatStencil8             = TextureFormat(gfx.TextureFormatStencil8)
	TextureFormatDepth16Unorm         = TextureFormat(gfx.TextureFormatDepth16Unorm)
	TextureFormatDepth24Plus          = TextureFormat(gfx.TextureFormatDepth24Plus)
	TextureFormatDepth24PlusStencil8  = TextureFormat(gfx.TextureFormatDepth24PlusStencil8)
	TextureFormatDepth32Float         = TextureFormat(gfx.TextureFormatDepth32Float)
	TextureFormatDepth32FloatStencil8 = TextureFormat(gfx.TextureFormatDepth32FloatStencil8)
)

// textureFormatFlags is what a texture format allows beyond being sampled
// and copied, on WebGPU's core, with no feature asked for.
type textureFormatFlags uint8

const (
	// textureFormatFlagColor is a colour format a pass may draw into.
	textureFormatFlagColor textureFormatFlags = 1 << iota
	// textureFormatFlagStorage is a format a storage binding may take.
	textureFormatFlagStorage
	// textureFormatFlagDepth is a format with a depth aspect, and
	// textureFormatFlagStencil one with a stencil aspect; a pass may use
	// either as its depth-stencil target.
	textureFormatFlagDepth
	textureFormatFlagStencil
)

// textureFormatFlagsByFormat is what each format allows; a format missing
// from it allows nothing beyond being sampled and copied.
var textureFormatFlagsByFormat = [...]textureFormatFlags{
	TextureFormatR8Unorm:              textureFormatFlagColor,
	TextureFormatR8Uint:               textureFormatFlagColor,
	TextureFormatR8Sint:               textureFormatFlagColor,
	TextureFormatR16Uint:              textureFormatFlagColor,
	TextureFormatR16Sint:              textureFormatFlagColor,
	TextureFormatR16Float:             textureFormatFlagColor,
	TextureFormatRG8Unorm:             textureFormatFlagColor,
	TextureFormatRG8Uint:              textureFormatFlagColor,
	TextureFormatRG8Sint:              textureFormatFlagColor,
	TextureFormatR32Float:             textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatR32Uint:              textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatR32Sint:              textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRG16Uint:             textureFormatFlagColor,
	TextureFormatRG16Sint:             textureFormatFlagColor,
	TextureFormatRG16Float:            textureFormatFlagColor,
	TextureFormatRGBA8Unorm:           textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA8UnormSrgb:       textureFormatFlagColor,
	TextureFormatRGBA8Snorm:           textureFormatFlagStorage,
	TextureFormatRGBA8Uint:            textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA8Sint:            textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatBGRA8Unorm:           textureFormatFlagColor,
	TextureFormatBGRA8UnormSrgb:       textureFormatFlagColor,
	TextureFormatRGB10A2Uint:          textureFormatFlagColor,
	TextureFormatRGB10A2Unorm:         textureFormatFlagColor,
	TextureFormatRG32Float:            textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRG32Uint:             textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRG32Sint:             textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA16Uint:           textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA16Sint:           textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA16Float:          textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA32Float:          textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA32Uint:           textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatRGBA32Sint:           textureFormatFlagColor | textureFormatFlagStorage,
	TextureFormatStencil8:             textureFormatFlagStencil,
	TextureFormatDepth16Unorm:         textureFormatFlagDepth,
	TextureFormatDepth24Plus:          textureFormatFlagDepth,
	TextureFormatDepth24PlusStencil8:  textureFormatFlagDepth | textureFormatFlagStencil,
	TextureFormatDepth32Float:         textureFormatFlagDepth,
	TextureFormatDepth32FloatStencil8: textureFormatFlagDepth | textureFormatFlagStencil,
}

// flags answers what f allows beyond being sampled and copied.
func (f TextureFormat) flags() textureFormatFlags {
	if int(f) >= len(textureFormatFlagsByFormat) {
		return 0
	}
	return textureFormatFlagsByFormat[f]
}

// makeTextureUsage answers every usage a texture of f with sampleCount
// samples may carry.
func (f TextureFormat) makeTextureUsage(sampleCount uint32) gfx.TextureUsageFlags {
	usage := gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagCopyDst | gfx.TextureUsageFlagTextureBinding
	flags := f.flags()
	if flags&(textureFormatFlagColor|textureFormatFlagDepth|textureFormatFlagStencil) != 0 {
		usage |= gfx.TextureUsageFlagRenderAttachment
	}
	if flags&textureFormatFlagStorage != 0 && sampleCount == 1 {
		usage |= gfx.TextureUsageFlagStorageBinding
	}
	return usage
}
