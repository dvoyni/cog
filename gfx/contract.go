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

// TextureFormat enumerates the pixel formats the renderer can create.
type TextureFormat uint8

const (
	// FormatRGBA8 is 8-bit-per-channel straight-alpha RGBA.
	FormatRGBA8 TextureFormat = iota
)

// AddressMode is a per-axis bitmask selecting how texture coordinates outside
// [0,1] are sampled: clamp by default, or repeat independently on the U and V
// axes. AddressRepeat repeats both axes.
type AddressMode uint8

const (
	AddressClamp   AddressMode = 0
	AddressRepeatX AddressMode = 1 << 0
	AddressRepeatY AddressMode = 1 << 1
	AddressRepeat  AddressMode = AddressRepeatX | AddressRepeatY
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

// Region is a rectangular sub-area of a texture in texels.
type Region struct{ X, Y, Width, Height int }

// TextureDesc describes a texture to create. Layers <= 1 creates a regular 2D
// texture; larger values create a 2D-array texture.
type TextureDesc struct {
	Width, Height int
	Layers        int
	Format        TextureFormat
	Mipmaps       bool
	Label         string
}

// TextureViewDimension selects the texture view expected by a shader binding.
type TextureViewDimension uint8

const (
	TextureView2D TextureViewDimension = iota
	TextureView2DArray
)

// SamplerDesc describes a sampler to create.
type SamplerDesc struct {
	Address AddressMode
	Filter  FilterMode
	Label   string
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

// ShaderResource is a reflected texture, sampler, or storage-buffer binding.
type ShaderResource struct {
	Name           string
	Sampler        bool
	StorageBuffer  bool
	WritableBuffer bool
	TextureView    TextureViewDimension
	Group          int
	Binding        int
}

// BufferDesc describes a GPU buffer to create. Dynamic marks a buffer whose
// contents the renderer rewrites every frame.
type BufferDesc struct {
	Kind    BufferKind
	Size    int
	Dynamic bool
	Label   string
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
	Shader     ShaderID
	Topology   PrimitiveTopology
	Blend      BlendMode
	DepthTest  bool
	Stride     int
	Attributes []VertexAttribute
	Label      string
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

	// ScreenFramebuffer returns the current frame's screen render target and its
	// physical size. It is valid only while a frame is being rendered (the driver
	// makes the surface current before triggering the render).
	ScreenFramebuffer() (view TextureViewID, width, height int)

	// Execute replays one already-translated queue into target: it performs bake
	// operations, begins a render pass using the queue's independent color/depth
	// clear masks, applies render ops in order, submits, then performs releases.
	// queue is owned by the caller and valid only for the duration of the call.
	Execute(target TextureViewID, queue *GpuQueue)
}
