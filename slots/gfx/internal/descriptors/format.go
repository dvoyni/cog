package descriptors

import "fmt"

// TextureFormat enumerates the pixel formats the renderer can create. The
// engine is linear, so the format is what says whether the bytes in a texture
// are light or a gamma-encoded picker value, and callers name it rather than
// inherit a default that is wrong half the time.
type TextureFormat uint8

const (
	// FormatRGBA8 is 8-bit-per-channel straight-alpha RGBA holding linear
	// values: normal, metallic-roughness and occlusion maps.
	FormatRGBA8 TextureFormat = iota
	// FormatRGBA8Srgb is the same layout holding gamma-encoded values the
	// hardware decodes on read: base colour, emissive and the canvas atlas.
	FormatRGBA8Srgb
	// FormatDepth32F is the one depth format, renderable and sampleable. There
	// is no stencil aspect anywhere in the engine.
	FormatDepth32F
	// FormatScreen is the sentinel for "whatever the frame buffer is", so a
	// pipeline can be keyed before the frame buffer exists. It resolves to
	// FrameBufferFormat.
	FormatScreen
)

// FrameBufferFormat is what every ScreenTarget pass renders into: the frame
// buffer gfx owns, which the implicit present pass then puts on the swapchain.
// The swapchain itself is unreachable as an sRGB surface - gogpu hardcodes
// BGRA8Unorm and exposes no view formats, and bgra8unorm-srgb is not a legal
// canvas-context format on the web - so the engine has to own this buffer to
// have any say over the colour space at all.
//
// It is sRGB, which is what makes the engine linear. Recorders write light:
// canvas samples an sRGB atlas, so its texels arrive decoded, and every colour
// a caller hands in is linear by the time it reaches a uniform. Light stored
// raw in a unorm buffer renders too dark, so the buffer encodes on store and
// the hardware does it. The present pass reads this same constant to decide
// whether it applies the sRGB OETF, so the buffer's colour space and the
// transfer function that puts it on screen stay one decision rather than two
// that can disagree.
const FrameBufferFormat = FormatRGBA8Srgb

// Resolve replaces the FormatScreen sentinel with the concrete frame-buffer
// format and returns every other format unchanged.
func (f TextureFormat) Resolve() TextureFormat {
	if f == FormatScreen {
		return FrameBufferFormat
	}
	return f
}

// Name names a texture format for a message, the sentinel resolved first.
func (f TextureFormat) String() string {
	switch f.Resolve() {
	case FormatRGBA8:
		return "RGBA8"
	case FormatRGBA8Srgb:
		return "RGBA8 sRGB"
	case FormatDepth32F:
		return "depth"
	default:
		return fmt.Sprintf("texture format %d", f)
	}
}
