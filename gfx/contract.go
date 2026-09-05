// Package gfx is cog's driver-agnostic high-level renderer (renderer v2). It
// wraps low-level GPU primitives (texture, sampler, shader, buffer, pipeline)
// behind an opaque Backend realization interface and exposes declarative,
// high-level concepts to gameplay: Mesh, Material, OpQueue, Viewport.
//
// Gameplay records into the writable OpQueue kernel resource; the plugin rotates
// completed queues through an internal triple buffer and consumes the latest on
// the render thread. The plugin translates high-level commands
// into a backend-agnostic GpuQueue and hands it to Backend.Execute,
// so gameplay never touches a GPU API and the plugin never imports one.
package gfx

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
// It is deliberately a plain unorm buffer today. Recorders still write
// gamma-encoded values, and this format plus a pass-through present is the one
// combination that leaves the frame bit-for-bit what it was before the frame
// buffer existed: an sRGB buffer would encode those values a second time on
// write, and cancelling that in the present shader only re-quantises them.
// Flipping this constant to FormatRGBA8Srgb is what turns the engine linear.
// The present pass reads this same constant to decide whether it applies the
// sRGB OETF, so the buffer's colour space and the transfer function that puts
// it on screen stay one decision rather than two that can disagree.
const FrameBufferFormat = FormatRGBA8

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

// Region is a rectangular sub-area of a texture in texels.
type Region struct{ X, Y, Width, Height int }

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

// ShaderDesc describes a shader module to create from opaque, backend-specific
// source bytes (e.g. WGSL for the wgpu backend). The renderer never interprets
// Code; the app supplies it, typically via Plugin.RegisterShader.
type ShaderDesc struct {
	Code  []byte
	Label string
}

// ShaderLayout describes a shader's reflected bindings: the uniform parameter
// block (members + byte offsets, and its group/binding) plus texture and sampler
// resources. The translator packs params at their declared offsets and binds
// each resource by matching its name to a material parameter.
type ShaderLayout struct {
	UniformSize    int
	UniformGroup   int
	UniformBinding int
	Uniforms       []UniformMember
	Resources      []ShaderResource
}

// UniformMember is one member of the shader-parameter uniform block: its name and
// byte offset within the block.
type UniformMember struct {
	Name   string
	Offset int
}

// StorageMember is one top-level member of a reflected storage struct. Stride
// and Count are set for an array member - `lights: array<SceneLight, 16>` - and
// zero otherwise.
type StorageMember struct {
	Name   string
	Offset int
	Stride int
	Count  int
}

// Limits is the subset of the WebGPU limits gfx checks shaders against.
type Limits struct {
	MaxBindGroups                   int
	MaxStorageBuffersPerShaderStage int
	MaxStorageBufferBindingSize     int
	MaxUniformBufferBindingSize     int
	MaxBufferSize                   int
}

// DefaultLimits is the WebGPU spec floor every browser guarantees. It is the
// comparison target on purpose: a desktop adapter reports its hardware limits,
// where 200 storage buffers is ordinary, so checking a shader against the
// device it happens to run on passes builds that cannot run in a browser.
var DefaultLimits = Limits{
	MaxBindGroups:                   4,
	MaxStorageBuffersPerShaderStage: 8,
	MaxStorageBufferBindingSize:     128 << 20,
	MaxUniformBufferBindingSize:     64 << 10,
	MaxBufferSize:                   256 << 20,
}

// StorageAlignment is the offset alignment a storage binding requires. A record
// a draw binds a range of therefore pads up to a multiple of it - a pad, not a
// cap on what a record may hold.
const StorageAlignment = 256

// ShaderResource is a reflected texture, sampler, or storage-buffer binding.
type ShaderResource struct {
	Name           string
	Sampler        bool
	StorageBuffer  bool
	WritableBuffer bool
	// Depth marks a depth texture and Comparison a comparison sampler: WebGPU
	// types those bindings differently from a colour texture and its filtering
	// sampler, and binding one where the other is declared is an error.
	Depth       bool
	Comparison  bool
	TextureView TextureViewDimension
	Group       int
	Binding     int
	// Members is the reflected layout of a storage struct's top-level members,
	// which is how a recorder that declares no uniform block at all - scene -
	// packs its records.
	Members []StorageMember
}

// BufferDesc describes a GPU buffer to create.
type BufferDesc struct {
	Kind  BufferKind
	Size  int
	Label string
}

// VertexAttribute is one attribute of the interleaved vertex buffer supplied to a
// pipeline: its byte offset, element type, and shader @location.
type VertexAttribute struct {
	Offset   int
	Type     VertexType
	Location int
}

// PipelineDesc describes a render pipeline to create. Bind group layouts are
// derived by the backend from the shader's reflection; the vertex layout is
// supplied by the mesh via Stride and Attributes.
type PipelineDesc struct {
	Shader      ShaderID
	Topology    PrimitiveTopology
	State       MaterialState
	ColorFormat TextureFormat
	DepthFormat TextureFormat
	Stride      int
	Attributes  []VertexAttribute
	Label       string
}

// Backend is the low-level realization interface: a vendor-neutral, "wgpu-shaped"
// API that a driver (e.g. wgpu) implements. The gfx plugin holds one Backend and
// never imports a GPU library. All methods are called on the driver's render
// thread. Create methods mint an opaque handle synchronously.
type Backend interface {
	// NewTexture and NewBuffer reserve logical IDs. They are CPU-only and must be
	// safe to call from the recording thread; native objects are created lazily
	// when Execute processes the corresponding bake op.
	NewTexture() TextureID
	NewBuffer() BufferID

	NewSampler(SamplerDesc) (SamplerID, error)
	FreeSampler(id SamplerID)

	NewShader(ShaderDesc) (ShaderID, error)
	FreeShader(id ShaderID)
	// ShaderLayout returns the reflected uniform parameter layout of a shader.
	ShaderLayout(id ShaderID) ShaderLayout

	NewPipeline(PipelineDesc) (PipelineID, error)
	FreePipeline(id PipelineID)

	// ScreenFramebuffer returns the surface the present pass draws into and its
	// physical size, which is also the size the frame buffer is allocated at. It
	// is not what ScreenTarget resolves to - that is the frame buffer - and it is
	// valid only while a frame is being rendered (the driver makes the surface
	// current before triggering the render).
	ScreenFramebuffer() (view TextureViewID, width, height int)

	// TextureView returns a renderable view of one mip level of one layer of a
	// baked texture, cached per (texture, mip, layer).
	TextureView(texture TextureID, mip, layer int) TextureViewID

	// Limits reports the device's own limits. gfx checks shaders against
	// DefaultLimits, the web floor, and never against these: they are here to
	// say, in the report, what the device this build ran on allowed.
	Limits() Limits

	// Execute replays one already-translated queue: it performs bake operations,
	// encodes each pass in turn with its own attachments and load/store ops,
	// submits once, then performs releases. queue is owned by the caller and
	// valid only for the duration of the call.
	Execute(queue *GpuQueue)
}
