package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
)

func TestTextureFormatMapsEveryGfxFormat(t *testing.T) {
	cases := []struct {
		format cgfx.TextureFormat
		want   gputypes.TextureFormat
	}{
		{cgfx.FormatRGBA8, gputypes.TextureFormatRGBA8Unorm},
		{cgfx.FormatRGBA8Srgb, gputypes.TextureFormatRGBA8UnormSrgb},
		{cgfx.FormatDepth32F, gputypes.TextureFormatDepth32Float},
		{cgfx.FormatScreen, textureFormat(cgfx.FrameBufferFormat)},
	}
	for _, c := range cases {
		if got := textureFormat(c.format); got != c.want {
			t.Errorf("textureFormat(%v) = %v, want %v", c.format, got, c.want)
		}
	}
}

func TestBytesPerTexelComesFromTheFormat(t *testing.T) {
	for _, format := range []cgfx.TextureFormat{cgfx.FormatRGBA8, cgfx.FormatRGBA8Srgb, cgfx.FormatDepth32F, cgfx.FormatScreen} {
		if got := bytesPerTexel(format); got != 4 {
			t.Errorf("bytesPerTexel(%v) = %d, want 4", format, got)
		}
	}
}

func TestRenderableAndDepthTexturesCarryRenderAttachmentUsage(t *testing.T) {
	sampled := textureUsage(cgfx.TextureDesc{Format: cgfx.FormatRGBA8Srgb})
	if sampled&gputypes.TextureUsageRenderAttachment != 0 {
		t.Errorf("sampled texture usage = %v, want no RenderAttachment", sampled)
	}
	if sampled&gputypes.TextureUsageTextureBinding == 0 || sampled&gputypes.TextureUsageCopyDst == 0 {
		t.Errorf("sampled texture usage = %v, want TextureBinding and CopyDst", sampled)
	}

	renderable := textureUsage(cgfx.TextureDesc{Format: cgfx.FormatRGBA8Srgb, Renderable: true})
	if renderable&gputypes.TextureUsageRenderAttachment == 0 {
		t.Errorf("renderable texture usage = %v, want RenderAttachment", renderable)
	}

	// Depth is renderable and sampleable, but WebGPU forbids writing texels into
	// a depth32float texture, so it must not ask for CopyDst.
	depth := textureUsage(cgfx.TextureDesc{Format: cgfx.FormatDepth32F})
	if depth&gputypes.TextureUsageRenderAttachment == 0 || depth&gputypes.TextureUsageTextureBinding == 0 {
		t.Errorf("depth texture usage = %v, want RenderAttachment and TextureBinding", depth)
	}
	if depth&gputypes.TextureUsageCopyDst != 0 {
		t.Errorf("depth texture usage = %v, want no CopyDst", depth)
	}
}

func TestMipmapsAreRefusedForDepth(t *testing.T) {
	if mipmapsSupported(cgfx.FormatDepth32F) {
		t.Error("mipmapsSupported(FormatDepth32F) = true, want false: a box filter over depth is meaningless")
	}
	for _, format := range []cgfx.TextureFormat{cgfx.FormatRGBA8, cgfx.FormatRGBA8Srgb, cgfx.FormatScreen} {
		if !mipmapsSupported(format) {
			t.Errorf("mipmapsSupported(%v) = false, want true", format)
		}
	}
}

func TestSrgbMipsFilterInLinearLight(t *testing.T) {
	// A black texel beside a mid-grey one, gamma-encoded, with alpha 0 and 255.
	src := []byte{0, 0, 0, 0, 128, 128, 128, 255}
	dst, dw, dh := downsampleTexels(src, 2, 1, cgfx.FormatRGBA8Srgb)
	if dw != 1 || dh != 1 {
		t.Fatalf("downsample size = %dx%d, want 1x1", dw, dh)
	}
	// Decoding 128 gives ~0.216 of the light black has none of; half of that,
	// re-encoded, is ~92. Averaging the encoded bytes would give 64 — the mip
	// that is too dark, which is the bug this filter exists to avoid.
	if dst[0] < 91 || dst[0] > 93 {
		t.Errorf("sRGB mip texel = %d, want ~92 (encoded-byte average would be 64)", dst[0])
	}
	// Alpha is coverage, not light, so it averages verbatim.
	if dst[3] != 127 && dst[3] != 128 {
		t.Errorf("sRGB mip alpha = %d, want the plain average 127", dst[3])
	}
}

func TestLinearMipsStayOnThePlainBoxFilter(t *testing.T) {
	src := []byte{0, 0, 0, 0, 128, 128, 128, 255}
	dst, _, _ := downsampleTexels(src, 2, 1, cgfx.FormatRGBA8)
	if dst[0] != 64 {
		t.Errorf("linear mip texel = %d, want the plain average 64", dst[0])
	}
}
