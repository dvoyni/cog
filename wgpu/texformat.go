package wgpu

import (
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/gogpu/gputypes"
)

// textureFormat maps a gfx format onto the backend format. FormatScreen is
// resolved first, so the sentinel never reaches a texture descriptor.
func textureFormat(format cgfx.TextureFormat) gputypes.TextureFormat {
	switch format.Resolve() {
	case cgfx.FormatRGBA8Srgb:
		return gputypes.TextureFormatRGBA8UnormSrgb
	case cgfx.FormatDepth32F:
		return gputypes.TextureFormatDepth32Float
	default:
		return gputypes.TextureFormatRGBA8Unorm
	}
}

// colorTargetFormat is the format a pipeline's colour attachment really has.
// FormatScreen is the sentinel for "whatever the screen target is", and today
// that target is the swapchain itself.
func (b *gfxBackend) colorTargetFormat(format cgfx.TextureFormat) gputypes.TextureFormat {
	if format == cgfx.FormatScreen {
		return b.surfaceFormat
	}
	return textureFormat(format)
}

// bytesPerTexel is the row stride per texel of a format, which every upload
// derives its BytesPerRow from.
func bytesPerTexel(format cgfx.TextureFormat) int {
	return int(textureFormat(format).BlockCopySize())
}

// textureUsage picks the GPU usage flags a descriptor asks for. Depth is
// renderable and sampleable but never a copy destination: WebGPU forbids
// writing texels into a depth32float texture.
func textureUsage(desc cgfx.TextureDesc) gputypes.TextureUsage {
	usage := gputypes.TextureUsageTextureBinding
	if desc.Format.Resolve() == cgfx.FormatDepth32F {
		return usage | gputypes.TextureUsageRenderAttachment
	}
	usage |= gputypes.TextureUsageCopyDst
	if desc.Renderable {
		usage |= gputypes.TextureUsageRenderAttachment
	}
	return usage
}

// mipmapsSupported reports whether a mip chain can be generated for a format.
// Box-filtering depth is meaningless, so it is refused rather than approximated.
func mipmapsSupported(format cgfx.TextureFormat) bool {
	return format.Resolve() != cgfx.FormatDepth32F
}

// downsampleTexels halves an image for the next mip level, filtering in the
// space the format says its texels are in.
func downsampleTexels(src []byte, width, height int, format cgfx.TextureFormat) (dst []byte, dw, dh int) {
	if format.Resolve() == cgfx.FormatRGBA8Srgb {
		return downsampleSrgb(src, width, height)
	}
	return downsampleRGBA(src, width, height)
}

// srgbDecode is the byte-to-linear table of the sRGB transfer function, using
// the engine's one curve in m.
var srgbDecode = func() (table [256]float32) {
	for value := range table {
		table[value] = m.NewColorSrgb8(uint8(value), 0, 0, 0).R
	}
	return table
}()

// downsampleSrgb box-filters a gamma-encoded image to half size (min 1px),
// averaging the light the texels stand for rather than their encodings, which
// would come out too dark. Alpha is coverage, not light, so it averages
// verbatim.
func downsampleSrgb(src []byte, width, height int) (dst []byte, dw, dh int) {
	dw, dh = max(1, width/2), max(1, height/2)
	dst = make([]byte, dw*dh*4)
	for y := range dh {
		sy0, sy1 := min(y*2, height-1), min(y*2+1, height-1)
		for x := range dw {
			sx0, sx1 := min(x*2, width-1), min(x*2+1, width-1)
			corners := [4]int{(sy0*width + sx0) * 4, (sy0*width + sx1) * 4, (sy1*width + sx0) * 4, (sy1*width + sx1) * 4}
			var light [3]float32
			alpha := 0
			for _, corner := range corners {
				for c := range 3 {
					light[c] += srgbDecode[src[corner+c]]
				}
				alpha += int(src[corner+3])
			}
			red, green, blue, _ := m.NewColorLinear(light[0]/4, light[1]/4, light[2]/4, 0).Srgb8()
			texel := (y*dw + x) * 4
			dst[texel], dst[texel+1], dst[texel+2] = red, green, blue
			dst[texel+3] = byte(alpha / 4)
		}
	}
	return dst, dw, dh
}
