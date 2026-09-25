package gfx

import "github.com/dvoyni/cog/slots/gfx/internal"

// ShaderDescr describes a shader by inline source text (ShaderWithText) or a
// resource path (ShaderWithResource), resolved to bytes by the renderer. The
// two cases are told apart by which field carries the answer, so there is no
// source enum to read.
//
// A descriptor also carries its supply - the defines and const values the
// preprocessor resolves the source against. A root source plus one supply is
// one variant, and two supplies over one path are two shaders, so the supply is
// part of the descriptor's identity everywhere identity is decided.
type ShaderDescr = internal.ShaderDescr

// ShaderOption is one entry of a shader's supply: a define or a const. Build it
// with ShaderDefine or ShaderConst.
type ShaderOption = internal.ShaderOption

// ShaderLocation names one line of one shader source. Line is 1-based; a zero
// value means the supply, which has no source line to point at.
type ShaderLocation = internal.ShaderLocation

// ShaderSourceMap says where every line of a flattened shader module came from.
//
// It is a table rather than a per-output-line array because the flatten is
// line-preserving by construction: comments, consumed directives and skipped
// branches blank rather than being deleted, splicing only reorders whole runs,
// and a #const injects its const on the directive's own line. So the output is a
// concatenation of contiguous segments with 1:1 line correspondence inside each,
// which a table describes as precisely as an array would at roughly one entry
// per source. That property is the thing to preserve, not the data structure.
type ShaderSourceMap = internal.ShaderSourceMap

// ShaderSegment is one run of flattened output lines coming from one source.
//
// The hoisted WGSL §4 prologue is the one segment with no source
// correspondence: its Source is empty and its lines map to nothing.
type ShaderSegment = internal.ShaderSegment

// TextureDescr describes a texture by resource path (TextureWithResource),
// inline pixel bytes (TextureWithBytes), or a texture returned by
// ResourceQueue.BakeTexture. The three are told apart by which field is set,
// which is why there are no source markers beside it.
type TextureDescr = internal.TextureDescr

// BufferDescr describes a GPU buffer from inline bytes (BufferWithBytes) or a
// baked storage buffer returned by ResourceQueue.BakeBuffer.
type BufferDescr = internal.BufferDescr

const (
	BufferSourceBytes = internal.BufferSourceBytes
	BufferSourceBaked = internal.BufferSourceBaked
)

// ParameterDescr is one declarative shader parameter: a texture, buffer, color,
// scalar, vector, matrix, or sampler. Build it with the *Param constructors and
// pass it to Material or OpQueue.Draw.
type ParameterDescr = internal.ParameterDescr

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr = internal.MaterialDescr

// VertexAttr describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given. Build it with Attr.
type VertexAttr = internal.VertexAttr

// MeshDescr is CPU-side geometry for one draw: a single interleaved vertex array
// (and an optional index array at one of the two index widths) as buffer
// descriptors, a primitive topology, and the vertex layout. Build it with Mesh
// or MeshIndexed; its fields are unexported and read by the translator.
type MeshDescr = internal.MeshDescr

// Order places a pass in the frame's shared ordering space. gfx defines no
// conventions and reserves no ranges: recorders that must interleave - canvas
// layers and scene cameras - agree on numbers between themselves, because they
// record from separate update subscriptions and stream order between them is
// not defined.
type Order = internal.Order

// TargetDescr names a pass's colour attachment.
type TargetDescr = internal.TargetDescr

// DepthDescr names a pass's depth attachment.
type DepthDescr = internal.DepthDescr

// PassDescr declares one render pass: where it draws, in what order, and what
// happens to its attachments at either end.
type PassDescr = internal.PassDescr

// PassRef selects a pass declared earlier in the same frame. Its zero value
// refers to no pass.
type PassRef = internal.PassRef

// ResourceID underlies the opaque GPU handles below, which a Backend mints. The
// zero value of each means "none".
type ResourceID = internal.ResourceID

// TextureID identifies a logical texture. The backend creates its native GPU
// object lazily when it executes the first bake op for this ID.
type TextureID = internal.TextureID

// BufferID identifies a logical buffer. The backend creates its native GPU
// object lazily when it executes the first bake op for this ID.
type BufferID = internal.BufferID

// SamplerID references a texture sampler.
type SamplerID = internal.SamplerID

// ShaderID references a compiled shader module.
type ShaderID = internal.ShaderID

// PipelineID references a render pipeline (shader + vertex layout + state).
type PipelineID = internal.PipelineID

// TextureViewID references a renderable view (a render target). The screen
// framebuffer and offscreen render targets are both TextureViewIDs.
type TextureViewID = internal.TextureViewID

// TextureFormat enumerates the pixel formats the renderer can create. The
// engine is linear, so the format is what says whether the bytes in a texture
// are light or a gamma-encoded picker value, and callers name it rather than
// inherit a default that is wrong half the time.
type TextureFormat = internal.TextureFormat

const (
	// FormatRGBA8 is 8-bit-per-channel straight-alpha RGBA holding linear
	// values: normal, metallic-roughness and occlusion maps.
	FormatRGBA8 = internal.FormatRGBA8
	// FormatRGBA8Srgb is the same layout holding gamma-encoded values the
	// hardware decodes on read: base colour, emissive and the canvas atlas.
	FormatRGBA8Srgb = internal.FormatRGBA8Srgb
	// FormatDepth32F is the one depth format, renderable and sampleable. There
	// is no stencil aspect anywhere in the engine.
	FormatDepth32F = internal.FormatDepth32F
	// FormatScreen is the sentinel for "whatever the frame buffer is", so a
	// pipeline can be keyed before the frame buffer exists. It resolves to
	// FrameBufferFormat.
	FormatScreen = internal.FormatScreen
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
const FrameBufferFormat = internal.FrameBufferFormat

// BufferKind tags a buffer's role, which selects its GPU usage flags.
type BufferKind = internal.BufferKind

const (
	BufferVertex  = internal.BufferVertex
	BufferIndex   = internal.BufferIndex
	BufferUniform = internal.BufferUniform
	BufferStorage = internal.BufferStorage
)

// BufferDesc describes a GPU buffer to create.
type BufferDesc = internal.BufferDesc

// StorageAlignment is the offset alignment a storage binding requires. A record
// a draw binds a range of therefore pads up to a multiple of it - a pad, not a
// cap on what a record may hold.
const StorageAlignment = internal.StorageAlignment

// IndexWidth is how wide one element of an index buffer is. There are exactly
// two, fixed by the platform rather than chosen: WebGPU has no uint8 index
// format, so geometry that was authored at a byte per index is stored at two.
//
// Nothing about it is a fidelity call - an index is exact or it is broken - so
// gfx neither derives nor validates the choice: it carries whatever width the
// caller declared its bytes to be in, and the only thing it can check is that
// the bytes divide by it.
//
// The zero value is IndexUint32, the width that is legal for any mesh, so a
// descriptor built without naming one is wide rather than wrong.
type IndexWidth = internal.IndexWidth

const (
	IndexUint32 = internal.IndexUint32
	IndexUint16 = internal.IndexUint16
)

// PrimitiveTopology selects how vertices assemble into primitives.
type PrimitiveTopology = internal.PrimitiveTopology

const (
	TopologyTriangleList  = internal.TopologyTriangleList
	TopologyTriangleStrip = internal.TopologyTriangleStrip
	TopologyLineList      = internal.TopologyLineList
)

// VertexType is the element type of one attribute in the interleaved vertex
// array: float, half-float, normalized, or integer scalar/vector types. Names
// mirror the WebGPU vertex formats.
type VertexType = internal.VertexType

const (
	UnknownVertexType = internal.UnknownVertexType
	Float32           = internal.Float32
	Float32x2         = internal.Float32x2
	Float32x3         = internal.Float32x3
	Float32x4         = internal.Float32x4
	Float16x2         = internal.Float16x2
	Float16x4         = internal.Float16x4
	Uint8x2           = internal.Uint8x2
	Uint8x4           = internal.Uint8x4
	Sint8x2           = internal.Sint8x2
	Sint8x4           = internal.Sint8x4
	Unorm8x2          = internal.Unorm8x2
	Unorm8x4          = internal.Unorm8x4
	Snorm8x2          = internal.Snorm8x2
	Snorm8x4          = internal.Snorm8x4
	Uint16x2          = internal.Uint16x2
	Uint16x4          = internal.Uint16x4
	Sint16x2          = internal.Sint16x2
	Sint16x4          = internal.Sint16x4
	Unorm16x2         = internal.Unorm16x2
	Unorm16x4         = internal.Unorm16x4
	Snorm16x2         = internal.Snorm16x2
	Snorm16x4         = internal.Snorm16x4
	Uint32            = internal.Uint32
	Uint32x2          = internal.Uint32x2
	Uint32x3          = internal.Uint32x3
	Uint32x4          = internal.Uint32x4
	Sint32            = internal.Sint32
	Sint32x2          = internal.Sint32x2
	Sint32x3          = internal.Sint32x3
	Sint32x4          = internal.Sint32x4
	Unorm1010102      = internal.Unorm1010102
)

// VertexScalar is the scalar type an attribute presents to the shader once the
// hardware has decoded it, which is not the same thing as the type its bytes
// are stored in: every normalized format arrives as float however many bits it
// occupies, and only the integer formats arrive as integers.
type VertexScalar = internal.VertexScalar

const (
	// VertexScalarNone is the zero value: a type that decodes to nothing a
	// shader can read. No legal vertex format has it.
	VertexScalarNone  = internal.VertexScalarNone
	VertexScalarFloat = internal.VertexScalarFloat
	VertexScalarUint  = internal.VertexScalarUint
	VertexScalarSint  = internal.VertexScalarSint
)

// VertexAttribute is one attribute of the interleaved vertex buffer supplied to a
// pipeline: its byte offset, element type, and shader @location.
type VertexAttribute = internal.VertexAttribute

// AddressMode selects how texture coordinates outside [0,1] are sampled on one
// axis. It is an enum rather than a bitmask because mirroring is a third mode,
// not a combination of the other two, and a flag that reads as a combination is
// a flag that gets silently reinterpreted.
type AddressMode = internal.AddressMode

const (
	AddressClamp  = internal.AddressClamp
	AddressRepeat = internal.AddressRepeat
	AddressMirror = internal.AddressMirror
)

// FilterMode selects texture minification/magnification filtering.
type FilterMode = internal.FilterMode

const (
	FilterLinear  = internal.FilterLinear
	FilterNearest = internal.FilterNearest
)

// SamplerDesc describes a sampler to create. Its zero value clamps both axes
// and filters linearly at every step, and it stays comparable so the translator
// can dedup identical samplers - a glTF material with five textures whose
// samplers happen to match costs one GPU object.
type SamplerDesc = internal.SamplerDesc

// BlendMode selects color blending against the render target.
type BlendMode = internal.BlendMode

const (
	// BlendAlpha is straight-alpha over blending.
	BlendAlpha = internal.BlendAlpha
	// BlendOpaque overwrites the target (no blend).
	BlendOpaque = internal.BlendOpaque
	// BlendAdditive adds source color weighted by source alpha.
	BlendAdditive = internal.BlendAdditive
	// BlendMultiply multiplies source and destination color.
	BlendMultiply = internal.BlendMultiply
)

// CompareFunc is a depth or sampler comparison. The zero value passes
// everything, which is the WebGPU default and what a draw that ignores depth
// wants. Depth is conventional: near maps to 0, far to 1, so CompareLess keeps
// the nearer fragment.
type CompareFunc = internal.CompareFunc

const (
	CompareAlways       = internal.CompareAlways
	CompareNever        = internal.CompareNever
	CompareLess         = internal.CompareLess
	CompareLessEqual    = internal.CompareLessEqual
	CompareGreater      = internal.CompareGreater
	CompareGreaterEqual = internal.CompareGreaterEqual
	CompareEqual        = internal.CompareEqual
	CompareNotEqual     = internal.CompareNotEqual
)

// CullMode selects which faces a pipeline discards.
type CullMode = internal.CullMode

const (
	CullNone  = internal.CullNone
	CullFront = internal.CullFront
	CullBack  = internal.CullBack
)

// FrontFace selects the winding that counts as the front face. glTF requires
// the reversed winding on nodes whose transform has a negative determinant.
type FrontFace = internal.FrontFace

const (
	FrontCCW = internal.FrontCCW
	FrontCW  = internal.FrontCW
)

// MaterialState controls fixed render-pipeline state. Depth compare and depth
// write are independent because the states 3D needs most - test but do not
// write, or test with another compare - are inexpressible as one flag. Every
// zero value is both the WebGPU default and what the backend did before the
// field existed, so MaterialState{} renders as it always has.
type MaterialState = internal.MaterialState

// LoadOp says what a pass does with an attachment's existing contents.
type LoadOp = internal.LoadOp

const (
	// LoadPreserve keeps what is already in the attachment.
	LoadPreserve = internal.LoadPreserve
	// LoadClear overwrites it with the pass's clear value.
	LoadClear = internal.LoadClear
	// LoadDiscard declares the contents irrelevant, which lets the driver skip
	// reading them back in.
	LoadDiscard = internal.LoadDiscard
)

// StoreOp says whether a pass's results survive it.
type StoreOp = internal.StoreOp

const (
	StoreKeep    = internal.StoreKeep
	StoreDiscard = internal.StoreDiscard
)

// PassDesc is one render pass for the backend to encode. Screen selects the
// frame buffer, which only the backend can resolve because it is sized from the
// surface; Target names any other colour attachment, and zero means none.
type PassDesc = internal.PassDesc

// CaptureDesc names one colour target to read back. Screen selects the frame
// buffer, which only the backend can resolve; Texture names any other colour
// texture, and zero means none. It mirrors PassDesc's addressing exactly.
//
// A capture always reads mip 0, layer 0. Texture is a TextureID rather than a
// TextureViewID because a texture-to-buffer copy names a texture, and because
// TextureTransition.Texture already names one.
type CaptureDesc = internal.CaptureDesc

// Region is a rectangular sub-area of a texture in texels. The json tags are
// there because a region reaches an agent inside a frame snapshot, and the
// rest of that document is lowerCamel.
type Region = internal.Region

// TextureDesc describes a texture to create. Layers <= 1 creates a regular 2D
// texture; larger values create a 2D-array texture. Renderable asks for a
// texture a render pass can draw into as well as sample.
type TextureDesc = internal.TextureDesc

// TextureViewDimension selects the texture view expected by a shader binding.
type TextureViewDimension = internal.TextureViewDimension

const (
	TextureView2D      = internal.TextureView2D
	TextureView2DArray = internal.TextureView2DArray
)

// TextureUsage names the role a texture is in as far as the GPU's memory
// pipeline is concerned. It is deliberately just the roles gfx can put a
// texture in, not a mirror of the backend's usage flags.
type TextureUsage = internal.TextureUsage

const (
	// TextureUsageRenderAttachment is a texture being written as a pass's
	// colour or depth attachment.
	TextureUsageRenderAttachment = internal.TextureUsageRenderAttachment
	// TextureUsageTextureBinding is a texture being read by a shader.
	TextureUsageTextureBinding = internal.TextureUsageTextureBinding
	// TextureUsageCopySrc is a texture being read back into CPU-visible
	// memory. It is the third role rather than a reuse of the other two
	// because From must name the usage a texture is actually in: a layout
	// transition that names the wrong old layout is undefined behaviour, and
	// a backend silently inserting an unnamed barrier for a capture is
	// precisely the undeclared hazard this type exists to abolish.
	TextureUsageCopySrc = internal.TextureUsageCopySrc
)

// TextureTransition orders one texture's writes against its reads, or the other
// way round. The backend derives none of these itself: it tracks resources for
// lifetime and submit-time validation only, so a texture written as an
// attachment and then sampled is not ordered against those writes - not within
// one command encoder, and not across a submit boundary either. On Vulkan the
// sample then reads the image mid-write, which shows as a flicker that looks
// random, is not a CPU/GPU race, and is invisible to a capture taken while the
// app is redrawing.
//
// gfx is the layer that can see the hazard, because by translation time the
// frame's passes are sorted and merged and the write-then-read pairs are
// computable. From is the usage the texture is actually in, not a guess: a
// layout transition that names the wrong old layout is undefined behaviour.
type TextureTransition = internal.TextureTransition

// ShaderDesc describes a shader module to create from opaque, backend-specific
// source bytes (WGSL for the gogpu backend). gfx flattens a shader's sources
// before it hands Code over, so a backend never sees a preprocessor directive.
type ShaderDesc = internal.ShaderDesc

// ShaderLayout describes a shader's reflected bindings: the uniform parameter
// block (members + byte offsets, and its group/binding) plus texture and sampler
// resources. The translator packs params at their declared offsets and binds
// each resource by matching its name to a material parameter.
type ShaderLayout = internal.ShaderLayout

// ShaderVertexInput is one @location input of a shader's vertex stage: where it
// binds and what it declares, reduced to the pair a vertex format can be
// compared against.
//
// The declared type is carried as (Kind, Count) rather than as source text
// because that is what the comparison is over: a format decodes to a scalar
// kind and a component count, and nothing in a spelling like "vec3<f32>"
// survives into the hardware beyond those two numbers.
type ShaderVertexInput = internal.ShaderVertexInput

// UniformMember is one member of the shader-parameter uniform block: its name and
// byte offset within the block.
type UniformMember = internal.UniformMember

// StorageMember is one top-level member of a reflected storage struct. Stride
// and Count are set for an array member - `lights: array<SceneLight, 16>` - and
// zero otherwise.
type StorageMember = internal.StorageMember

// ShaderResource is a reflected texture, sampler, or storage-buffer binding.
type ShaderResource = internal.ShaderResource

// PipelineDesc describes a render pipeline to create. Bind group layouts are
// derived by the backend from the shader's reflection; the vertex layout is
// supplied by the mesh via Stride and Attributes.
type PipelineDesc = internal.PipelineDesc

// Limits is the subset of the WebGPU limits gfx checks shaders against.
type Limits = internal.Limits

// Capture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes and Image removes; a backend never sees an image.Image.
//
// One struct carries success and failure so that a caller cannot handle one and
// forget the other, which is how a capture that never arrives becomes a hang
// somewhere far away.
type Capture = internal.Capture

// Queue owns a translated command sequence. Commands are constructed as local
// values and appended once. Bakes are hoisted ahead of every pass; render
// commands belong to the pass that was open when they were recorded.
//
// gfx appends to it and a Backend reads it back only by replaying it into its
// sinks: ReplayBakes, ReplayPasses and ReplayReleases are the whole read side.
type Queue = internal.Queue

// UniformAlignment is where a block may start in the frame's uniform arena:
// every block's offset is a multiple of it. It is the largest
// minUniformBufferOffsetAlignment WebGPU permits, so an offset aligned to it is
// a valid uniform binding offset on every device. A Queue hands a backend the
// arena through BakeSink.BakeUniforms and binds a block per draw through
// RenderPass.SetUniformBlock(offset, size).
const UniformAlignment = internal.UniformAlignment

// PassSink receives the frame's passes. BeginPass returns the RenderPass its
// commands go to, so the backend owns encoder and pass lifetime entirely.
type PassSink = internal.PassSink

// BakeSink receives resource uploads before render-pass encoding.
type BakeSink = internal.BakeSink

// RenderPass receives render commands in recording order.
type RenderPass = internal.RenderPass

// ReleaseSink receives resource releases after submission.
type ReleaseSink = internal.ReleaseSink

// SnapshotView is what every snapshot response carries whatever it is a
// snapshot of: the three coordinate sizes, and whether producing it cost a
// step.
//
// All three sizes, not two. A capture reports pixels and window units and
// deliberately omits the logical viewport, because that is the game's own
// sizing policy and means nothing to an agent looking at a picture. A snapshot
// is the inverse case - its coordinates are in that space - so leaving it out
// breaks the find-the-button-click-the-button flow silently, which is the
// failure mode worth spending a field on.
type SnapshotView = internal.SnapshotView

// ParameterView is one shader parameter rendered for an agent: its name, which
// of the union's arms it is, and that arm's value alone.
type ParameterView = internal.ParameterView

// TextureView is one texture rendered for an agent: where it came from, how
// big it is, and how many bytes of pixels it is carrying - never the pixels.
//
// The name is the one the spec family agreed on across three tools. It is not
// a GPU texture view; that is TextureViewDimension and TextureViewID, which
// are a different thing gfx also has.
type TextureView = internal.TextureView

// BufferView is one buffer rendered for an agent, plus the slice of it a
// parameter binds when a parameter is what produced the view.
type BufferView = internal.BufferView

// SamplerView is one sampler rendered for an agent, with every mode named
// rather than numbered: a sampler is small enough to report whole, and an
// enum ordinal in a debug dump is a lookup an agent cannot perform.
type SamplerView = internal.SamplerView

// MaterialView is one material rendered for an agent: which shader variant
// shades the draw, the fixed pipeline state, and the material's own
// parameters, which a draw's same-named parameters override.
type MaterialView = internal.MaterialView

// ShaderView names one shader variant. A root source plus one supply is one
// variant, so the supply is part of the name rather than a detail beside it.
type ShaderView = internal.ShaderView

// MaterialStateView is fixed pipeline state with its enums named. Depth
// compare and depth write are separate here because they are separate in the
// engine: test but do not write is a state 3D needs and one flag cannot say.
type MaterialStateView = internal.MaterialStateView

// ViewportMode selects how the logical viewport responds to window aspect
// changes. ViewportWindow uses the window dimensions directly; fixed modes keep
// one dimension constant; Fit shows the full desired rectangle, while Cover
// fills the viewport from it.
type ViewportMode = internal.ViewportMode

const (
	ViewportWindow      = internal.ViewportWindow
	ViewportFixedWidth  = internal.ViewportFixedWidth
	ViewportFixedHeight = internal.ViewportFixedHeight
	ViewportFit         = internal.ViewportFit
	ViewportCover       = internal.ViewportCover
)

// FrameSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type FrameSnapshot = internal.FrameSnapshot

// FrameView is one tick's renderer declarations, rendered while they are still
// alive. It is not a copy of the queue: no queue outlives the tick that filled
// it, and between ticks the queue is empty rather than stale, so the view is
// produced inside the tick and shaped by the request that asked for it.
//
// Every index in it is a source index - a position in the queue that recorded
// the thing, never a position in the emitted array. Filtering makes the
// emitted array a subset, and if indices were positions in that subset every
// cross-reference would point at the wrong thing. With source indices the
// index is the address, and an elided pass stays addressable for free.
type FrameView = internal.FrameView

// PassView is one declared render pass: where it draws, in what order, what
// happens to its attachments at either end, and how much work it carries.
type PassView = internal.PassView

// ResourceOpView is one resource operation: what it does, to which handle, and
// how big the thing is. Bulk bytes are reported as a count and left where they
// are.
type ResourceOpView = internal.ResourceOpView

// inlineAnchor is never called. It exists so that gfx's importers can inline
// the accessors they call per instance - canvas per sprite, scene per draw,
// gogpu per pass. Go inlines a method of a package the caller does not import
// only when a package it does import references that method, and these
// methods are declared in gfx/internal, which nothing outside gfx can
// import. Referencing them here puts their bodies in this package's export
// data. The tier test allows this shape and nothing broader; see
// architecture.instructions.md.
func inlineAnchor(parameter ParameterDescr, texture TextureDescr, material MaterialDescr, format TextureFormat) {
	_ = parameter.Name()
	_, _ = parameter.ColorValue()
	_, _ = parameter.VecValue()
	_, _ = parameter.FloatValue()
	_, _ = parameter.TextureValue()
	_ = texture.ID()
	_, _ = texture.Size()
	_ = material.State()
	_ = format.Resolve()
}
