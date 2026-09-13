package wgpu

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
	"github.com/gogpu/gputypes"
)

// textureFormat maps a gfx format onto the backend format. FormatScreen is
// resolved first, so the sentinel never reaches a texture descriptor.
func textureFormat(format gpu.TextureFormat) gputypes.TextureFormat {
	switch format.Resolve() {
	case gpu.FormatRGBA8Srgb:
		return gputypes.TextureFormatRGBA8UnormSrgb
	case gpu.FormatDepth32F:
		return gputypes.TextureFormatDepth32Float
	default:
		return gputypes.TextureFormatRGBA8Unorm
	}
}

// bytesPerTexel is the row stride per texel of a format, which every upload
// derives its BytesPerRow from.
func bytesPerTexel(format gpu.TextureFormat) int {
	return int(textureFormat(format).BlockCopySize())
}

// textureUsage picks the GPU usage flags a descriptor asks for. Depth is
// renderable and sampleable but never a copy destination: WebGPU forbids
// writing texels into a depth32float texture.
//
// Renderable also grants CopySrc, which is what makes a capture possible: you
// can only read back what something rendered into, so Renderable already names
// exactly the capturable set and no new field has to predict it. The cost,
// stated so nobody finds it in a profile instead: on some drivers CopySrc
// disables lossless framebuffer compression on that texture, and it is paid
// whether or not a capture ever happens. It is bounded to render targets,
// which is why the alternative - granting it to every non-depth texture - was
// not taken: that pays on every atlas and material map in the scene.
func textureUsage(desc gpu.TextureDesc) gputypes.TextureUsage {
	usage := gputypes.TextureUsageTextureBinding
	if desc.Format.Resolve() == gpu.FormatDepth32F {
		return usage | gputypes.TextureUsageRenderAttachment
	}
	usage |= gputypes.TextureUsageCopyDst
	if desc.Renderable {
		usage |= gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc
	}
	return usage
}

// mipmapsSupported reports whether a mip chain can be generated for a format.
// Box-filtering depth is meaningless, so it is refused rather than approximated.
func mipmapsSupported(format gpu.TextureFormat) bool {
	return format.Resolve() != gpu.FormatDepth32F
}

// downsampleTexels halves an image for the next mip level, filtering in the
// space the format says its texels are in.
func downsampleTexels(src []byte, width, height int, format gpu.TextureFormat) (dst []byte, dw, dh int) {
	if format.Resolve() == gpu.FormatRGBA8Srgb {
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
