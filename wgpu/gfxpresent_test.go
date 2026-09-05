package wgpu

import (
	"strings"
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
)

func TestThePresentShaderTakesItsTransferFunctionFromTheFrameBufferFormat(t *testing.T) {
	// Sampling an sRGB frame buffer decodes it, so what arrives is light and
	// has to be encoded again for the unorm swapchain.
	if !strings.Contains(presentSource(cgfx.FormatRGBA8Srgb), "1.0 / 2.4") {
		t.Error("the sRGB frame buffer's present shader does not apply the OETF")
	}
	// A unorm frame buffer arrives exactly as it was written, and encoding it
	// here would encode a second time what the recorders already encoded.
	if strings.Contains(presentSource(cgfx.FormatRGBA8), "1.0 / 2.4") {
		t.Error("the unorm frame buffer's present shader encodes an already-encoded frame")
	}
	// FormatScreen is the sentinel, not a third answer.
	if presentSource(cgfx.FormatScreen) != presentSource(cgfx.FrameBufferFormat) {
		t.Error("FormatScreen and the frame buffer format chose different present shaders")
	}
}

func TestBothPresentShadersCompile(t *testing.T) {
	// Neither variant is reachable from any test that runs on a GPU, and the
	// unreachable one is the whole point of writing it now: the colour flip
	// changes one constant, and this is what says the other branch was legal
	// WGSL when it was written.
	for _, format := range []cgfx.TextureFormat{cgfx.FormatRGBA8, cgfx.FormatRGBA8Srgb} {
		if _, err := lowerWGSL(presentSource(format)); err != nil {
			t.Errorf("present shader for %v does not compile: %v", format, err)
		}
	}
}

func TestThePresentShaderDrawsOneFullScreenTriangle(t *testing.T) {
	source := presentSource(cgfx.FrameBufferFormat)
	// No vertex buffer: the triangle is derived from the vertex index, which is
	// why Present can draw three vertices and bind nothing but the buffer.
	if !strings.Contains(source, "@builtin(vertex_index)") {
		t.Error("the present shader does not build its triangle from the vertex index")
	}
	for _, entry := range []string{"vs_main", "fs_main", "frameTexture", "frameSampler"} {
		if !strings.Contains(source, entry) {
			t.Errorf("the present shader is missing %q", entry)
		}
	}
}
