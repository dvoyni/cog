package internal

// Opaque GPU handles minted by a Backend. The zero value means "none".
type (
	ResourceID uint32
	// TextureID identifies a logical texture. The backend creates its native GPU
	// object lazily when it executes the first bake op for this ID.
	TextureID ResourceID
	// BufferID identifies a logical buffer. The backend creates its native GPU
	// object lazily when it executes the first bake op for this ID.
	BufferID ResourceID
	// SamplerID references a texture sampler.
	SamplerID ResourceID
	// ShaderID references a compiled shader module.
	ShaderID ResourceID
	// PipelineID references a render pipeline (shader + vertex layout + state).
	PipelineID ResourceID
	// TextureViewID references a renderable view (a render target). The screen
	// framebuffer and offscreen render targets are both TextureViewIDs.
	TextureViewID ResourceID
)

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

// AddressMode selects how texture coordinates outside [0,1] are sampled on one
// axis. It is an enum rather than a bitmask because mirroring is a third mode,
// not a combination of the other two, and a flag that reads as a combination is
// a flag that gets silently reinterpreted.
type AddressMode uint8

const (
	AddressClamp AddressMode = iota
	AddressRepeat
	AddressMirror
)

// FilterMode selects texture minification/magnification filtering.
type FilterMode uint8

const (
	FilterLinear FilterMode = iota
	FilterNearest
)

// BufferKind tags a buffer's role, which selects its GPU usage flags.
type BufferKind uint8

const (
	BufferVertex BufferKind = iota
	BufferIndex
	BufferUniform
	BufferStorage
)

// PrimitiveTopology selects how vertices assemble into primitives.
type PrimitiveTopology uint8

const (
	TopologyTriangleList PrimitiveTopology = iota
	TopologyTriangleStrip
	TopologyLineList
)

// BlendMode selects color blending against the render target.
type BlendMode uint8

const (
	// BlendAlpha is straight-alpha over blending.
	BlendAlpha BlendMode = iota
	// BlendOpaque overwrites the target (no blend).
	BlendOpaque
	// BlendAdditive adds source color weighted by source alpha.
	BlendAdditive
	// BlendMultiply multiplies source and destination color.
	BlendMultiply
)

// CompareFunc is a depth or sampler comparison. The zero value passes
// everything, which is the WebGPU default and what a draw that ignores depth
// wants. Depth is conventional: near maps to 0, far to 1, so CompareLess keeps
// the nearer fragment.
type CompareFunc uint8

const (
	CompareAlways CompareFunc = iota
	CompareNever
	CompareLess
	CompareLessEqual
	CompareGreater
	CompareGreaterEqual
	CompareEqual
	CompareNotEqual
)

// CullMode selects which faces a pipeline discards.
type CullMode uint8

const (
	CullNone CullMode = iota
	CullFront
	CullBack
)

// FrontFace selects the winding that counts as the front face. glTF requires
// the reversed winding on nodes whose transform has a negative determinant.
type FrontFace uint8

const (
	FrontCCW FrontFace = iota
	FrontCW
)

// Region is a rectangular sub-area of a texture in texels. The json tags are
// there because a region reaches an agent inside a frame snapshot, and the
// rest of that document is lowerCamel.
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TextureDesc describes a texture to create. Layers <= 1 creates a regular 2D
// texture; larger values create a 2D-array texture. Renderable asks for a
// texture a render pass can draw into as well as sample.
type TextureDesc struct {
	Width, Height int
	Layers        int
	Format        TextureFormat
	Mipmaps       bool
	Renderable    bool
	Label         string
}

// TextureViewDimension selects the texture view expected by a shader binding.
type TextureViewDimension uint8

const (
	TextureView2D TextureViewDimension = iota
	TextureView2DArray
)

// SamplerDesc describes a sampler to create. Its zero value clamps both axes
// and filters linearly at every step, and it stays comparable so the translator
// can dedup identical samplers - a glTF material with five textures whose
// samplers happen to match costs one GPU object.
type SamplerDesc struct {
	AddressU, AddressV AddressMode
	// Mag, Min and Mip are separate because glTF specifies magnification,
	// minification and mip selection independently. Zero is FilterLinear.
	Mag, Min, Mip FilterMode
	// Anisotropy is the maximum anisotropic sample count. 0 and 1 both mean
	// off, and it is clamped to 16. WebGPU requires all three filters linear
	// whenever it is above 1.
	Anisotropy uint8
	// Comparison makes this a comparison sampler, which a shadow map needs and
	// which cannot be the same object as a colour sampler.
	Comparison bool
	Compare    CompareFunc
	Label      string
}
