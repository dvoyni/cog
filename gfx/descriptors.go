package gfx

import (
	"fmt"
	"slices"
)

// Every descriptor mirrors wgpu v0.34.2's, with gfx's handles in place of
// wgpu's objects. An optional part is a value whose zero means none, never a
// pointer; slices stay slices where the count has no small bound.

// Extent3D is a size in texels.
type Extent3D struct {
	Width, Height, DepthOrArrayLayers uint32
}

// Origin3D is a texel position.
type Origin3D struct {
	X, Y, Z uint32
}

// ImageDataLayout is the layout of texel data in a buffer or a byte slice.
type ImageDataLayout struct {
	Offset                    uint64
	BytesPerRow, RowsPerImage uint32
}

// BufferDescriptor describes a buffer to create.
type BufferDescriptor struct {
	Label            string
	Size             uint64
	Usage            BufferUsageFlags
	MappedAtCreation bool
}

// TextureDescriptor describes a texture to create. Device.TextureDescriptor
// answers it back, since wgpu keeps only the format.
type TextureDescriptor struct {
	Label                      string
	Size                       Extent3D
	MipLevelCount, SampleCount uint32
	Dimension                  TextureDimension
	Format                     TextureFormat
	Usage                      TextureUsageFlags
	ViewFormats                []TextureFormat
}

// TextureViewDescriptor describes a view of a texture.
type TextureViewDescriptor struct {
	Label                                                        string
	Format                                                       TextureFormat
	Dimension                                                    TextureViewDimension
	Aspect                                                       TextureAspect
	BaseMipLevel, MipLevelCount, BaseArrayLayer, ArrayLayerCount uint32
}

// SamplerDescriptor describes a sampler. A zero Compare is no comparison.
type SamplerDescriptor struct {
	Label                                    string
	AddressModeU, AddressModeV, AddressModeW AddressMode
	MagFilter, MinFilter, MipmapFilter       FilterMode
	LodMinClamp, LodMaxClamp                 float32
	Compare                                  CompareFunction
	Anisotropy                               uint16
}

// ShaderModuleDescriptor describes a shader module: final WGSL, already
// through shader.Preprocess when it uses directives.
type ShaderModuleDescriptor struct {
	Label string
	WGSL  string
}

// CommandEncoderDescriptor describes a standalone command encoder.
type CommandEncoderDescriptor struct {
	Label string
}

// BindGroupLayoutDescriptor describes a bind-group layout.
type BindGroupLayoutDescriptor struct {
	Label   string
	Entries []BindGroupLayoutEntry
}

// BindGroupLayoutEntry is one binding of a layout. Exactly one of Buffer,
// Sampler, Texture and StorageTexture is non-zero.
type BindGroupLayoutEntry struct {
	Binding        uint32
	Visibility     ShaderStageFlags
	Buffer         BufferBindingLayout
	Sampler        SamplerBindingLayout
	Texture        TextureBindingLayout
	StorageTexture StorageTextureBindingLayout
}

// BufferBindingLayout is a buffer binding; zero Type is none.
type BufferBindingLayout struct {
	Type             BufferBindingType
	HasDynamicOffset bool
	MinBindingSize   uint64
}

// SamplerBindingLayout is a sampler binding; zero Type is none.
type SamplerBindingLayout struct {
	Type SamplerBindingType
}

// TextureBindingLayout is a sampled-texture binding; zero SampleType is none.
type TextureBindingLayout struct {
	SampleType    TextureSampleType
	ViewDimension TextureViewDimension
	Multisampled  bool
}

// StorageTextureBindingLayout is a storage-texture binding; zero Access is
// none.
type StorageTextureBindingLayout struct {
	Access        StorageTextureAccess
	Format        TextureFormat
	ViewDimension TextureViewDimension
}

// BindGroupDescriptor describes a bind group.
type BindGroupDescriptor struct {
	Label   string
	Layout  BindGroupLayout
	Entries []BindGroupEntry
}

// BindGroupEntry binds one resource: exactly one of Buffer, Sampler and
// TextureView is non-zero. A zero Size is the rest of the buffer.
type BindGroupEntry struct {
	Binding      uint32
	Buffer       Buffer
	Offset, Size uint64
	Sampler      Sampler
	TextureView  TextureView
}

// PipelineLayoutDescriptor describes a pipeline layout.
type PipelineLayoutDescriptor struct {
	Label            string
	BindGroupLayouts []BindGroupLayout
}

// RenderPipelineDescriptor describes a render pipeline. A zero Layout is
// WebGPU's "auto": gfx creates the layouts the backend reflects over both
// stages, and Device.RenderPipelineBindGroupLayout answers them.
type RenderPipelineDescriptor struct {
	Label        string
	Layout       PipelineLayout
	Vertex       VertexState
	Primitive    PrimitiveState
	DepthStencil DepthStencilState
	Multisample  MultisampleState
	Fragment     FragmentState
}

// clone answers a copy of descriptor that shares no slice with it.
func (descriptor RenderPipelineDescriptor) clone() RenderPipelineDescriptor {
	descriptor.Vertex.Buffers = slices.Clone(descriptor.Vertex.Buffers)
	for i := range descriptor.Vertex.Buffers {
		descriptor.Vertex.Buffers[i].Attributes = slices.Clone(descriptor.Vertex.Buffers[i].Attributes)
	}
	descriptor.Fragment.Targets = slices.Clone(descriptor.Fragment.Targets)
	return descriptor
}

// VertexState is a render pipeline's vertex stage.
type VertexState struct {
	Module     ShaderModule
	EntryPoint string
	Buffers    []VertexBufferLayout
}

// VertexBufferLayout is the layout of one vertex buffer.
type VertexBufferLayout struct {
	ArrayStride uint64
	StepMode    VertexStepMode
	Attributes  []VertexAttribute
}

// VertexAttribute is one attribute of a vertex buffer.
type VertexAttribute struct {
	Format         VertexFormat
	Offset         uint64
	ShaderLocation uint32
}

// PrimitiveState is how primitives are assembled and rasterized; its zero
// value is the spec default. A zero StripIndexFormat is none.
type PrimitiveState struct {
	Topology         PrimitiveTopology
	StripIndexFormat IndexFormat
	FrontFace        FrontFace
	CullMode         CullMode
	UnclippedDepth   bool
}

// DepthStencilState is a render pipeline's depth and stencil tests; a zero
// Format is no depth-stencil attachment.
type DepthStencilState struct {
	Format                              TextureFormat
	DepthWriteEnabled                   bool
	DepthCompare                        CompareFunction
	StencilFront, StencilBack           StencilFaceState
	StencilReadMask, StencilWriteMask   uint32
	DepthBias                           int32
	DepthBiasSlopeScale, DepthBiasClamp float32
}

// StencilFaceState is the stencil test of one face.
type StencilFaceState struct {
	Compare                     CompareFunction
	FailOp, DepthFailOp, PassOp StencilOperation
}

// MultisampleState is a render pipeline's multisampling; a zero Count is 1.
type MultisampleState struct {
	Count                  uint32
	Mask                   uint64
	AlphaToCoverageEnabled bool
}

// FragmentState is a render pipeline's fragment stage; a zero Module is no
// fragment stage.
type FragmentState struct {
	Module     ShaderModule
	EntryPoint string
	Targets    []ColorTargetState
}

// ColorTargetState is one color target of a render pipeline; a zero Blend is
// no blending.
type ColorTargetState struct {
	Format    TextureFormat
	Blend     BlendState
	WriteMask ColorWriteFlags
}

// BlendState is a color target's blending.
type BlendState struct {
	Color, Alpha BlendComponent
}

// BlendComponent is the blending of the color or the alpha channel.
type BlendComponent struct {
	SrcFactor, DstFactor BlendFactor
	Operation            BlendOperation
}

// ComputePipelineDescriptor describes a compute pipeline. A zero Layout is
// "auto", as for render pipelines. Constants are the module's override
// constants by name; under -tags rust they are a gap, since that build of
// wgpu lacks them.
type ComputePipelineDescriptor struct {
	Label      string
	Layout     PipelineLayout
	Module     ShaderModule
	EntryPoint string
	Constants  map[string]float64
}

// RenderPassDescriptor describes a render pass. A zero DepthStencilAttachment
// View is no depth-stencil attachment.
type RenderPassDescriptor struct {
	Label                  string
	ColorAttachments       []RenderPassColorAttachment
	DepthStencilAttachment RenderPassDepthStencilAttachment
	// TimestampWrites is where the pass writes its timestamps; zero is none.
	TimestampWrites PassTimestampWrites
}

// RenderPassColorAttachment is one color attachment; a zero ResolveTarget is
// none. A window's framebuffer is the TextureView from Device.Framebuffer.
type RenderPassColorAttachment struct {
	View, ResolveTarget TextureView
	LoadOp              LoadOp
	StoreOp             StoreOp
	ClearValue          Color
}

// RenderPassDepthStencilAttachment is a render pass's depth-stencil
// attachment.
type RenderPassDepthStencilAttachment struct {
	View              TextureView
	DepthLoadOp       LoadOp
	DepthStoreOp      StoreOp
	DepthClearValue   float32
	DepthReadOnly     bool
	StencilLoadOp     LoadOp
	StencilStoreOp    StoreOp
	StencilClearValue uint32
	StencilReadOnly   bool
}

// ComputePassDescriptor describes a compute pass.
type ComputePassDescriptor struct {
	Label string
	// TimestampWrites is where the pass writes its timestamps; zero is none.
	TimestampWrites PassTimestampWrites
}

// ImageCopyTexture is a texture region's origin, for copies and writes.
type ImageCopyTexture struct {
	Texture  Texture
	MipLevel uint32
	Origin   Origin3D
	Aspect   TextureAspect
}

// TextureCopy is one region of a texture-to-texture copy.
type TextureCopy struct {
	Source, Destination ImageCopyTexture
	Size                Extent3D
}

// BufferTextureCopy is one region of a copy between a buffer and a texture.
type BufferTextureCopy struct {
	BufferLayout ImageDataLayout
	TextureBase  ImageCopyTexture
	Size         Extent3D
}

// Color is a color with float64 channels, as wgpu's clear values are.
type Color struct {
	R, G, B, A float64
}

// Viewport is a render pass's viewport.
type Viewport struct {
	X, Y, Width, Height, MinDepth, MaxDepth float32
}

// ScissorRect is a render pass's scissor rectangle.
type ScissorRect struct {
	X, Y, Width, Height uint32
}

// DrawArgs are the arguments of a non-indexed draw.
type DrawArgs struct {
	VertexCount, InstanceCount, FirstVertex, FirstInstance uint32
}

// DrawIndexedArgs are the arguments of an indexed draw.
type DrawIndexedArgs struct {
	IndexCount, InstanceCount, FirstIndex uint32
	BaseVertex                            int32
	FirstInstance                         uint32
}

// AdapterInfo describes the GPU the device runs on.
type AdapterInfo struct {
	Name, Vendor       string
	VendorID, DeviceID uint32
	DeviceType         DeviceType
	Driver, DriverInfo string
	Backend            Backend
}

// Limits are a device's limits, the WebGPU set; push constants, binding
// arrays and ray tracing, which gfx does not offer, are left out.
type Limits struct {
	MaxTextureDimension1D                     uint32
	MaxTextureDimension2D                     uint32
	MaxTextureDimension3D                     uint32
	MaxTextureArrayLayers                     uint32
	MaxBindGroups                             uint32
	MaxBindGroupsPlusVertexBuffers            uint32
	MaxBindingsPerBindGroup                   uint32
	MaxDynamicUniformBuffersPerPipelineLayout uint32
	MaxDynamicStorageBuffersPerPipelineLayout uint32
	MaxSampledTexturesPerShaderStage          uint32
	MaxSamplersPerShaderStage                 uint32
	MaxStorageBuffersPerShaderStage           uint32
	MaxStorageTexturesPerShaderStage          uint32
	MaxUniformBuffersPerShaderStage           uint32
	MaxUniformBufferBindingSize               uint64
	MaxStorageBufferBindingSize               uint64
	MinUniformBufferOffsetAlignment           uint32
	MinStorageBufferOffsetAlignment           uint32
	MaxVertexBuffers                          uint32
	MaxBufferSize                             uint64
	MaxVertexAttributes                       uint32
	MaxVertexBufferArrayStride                uint32
	MaxInterStageShaderVariables              uint32
	MaxColorAttachments                       uint32
	MaxColorAttachmentBytesPerSample          uint32
	MaxComputeWorkgroupStorageSize            uint32
	MaxComputeInvocationsPerWorkgroup         uint32
	MaxComputeWorkgroupSizeX                  uint32
	MaxComputeWorkgroupSizeY                  uint32
	MaxComputeWorkgroupSizeZ                  uint32
	MaxComputeWorkgroupsPerDimension          uint32
}

// DefaultLimits answers WebGPU's default limits, which gfx requests when
// Config.RequiredLimits is zero.
func DefaultLimits() Limits {
	return Limits{
		MaxTextureDimension1D:                     8192,
		MaxTextureDimension2D:                     8192,
		MaxTextureDimension3D:                     2048,
		MaxTextureArrayLayers:                     256,
		MaxBindGroups:                             4,
		MaxBindGroupsPlusVertexBuffers:            24,
		MaxBindingsPerBindGroup:                   1000,
		MaxDynamicUniformBuffersPerPipelineLayout: 8,
		MaxDynamicStorageBuffersPerPipelineLayout: 4,
		MaxSampledTexturesPerShaderStage:          16,
		MaxSamplersPerShaderStage:                 16,
		MaxStorageBuffersPerShaderStage:           8,
		MaxStorageTexturesPerShaderStage:          4,
		MaxUniformBuffersPerShaderStage:           12,
		MaxUniformBufferBindingSize:               65536,
		MaxStorageBufferBindingSize:               134217728,
		MinUniformBufferOffsetAlignment:           256,
		MinStorageBufferOffsetAlignment:           256,
		MaxVertexBuffers:                          8,
		MaxBufferSize:                             268435456,
		MaxVertexAttributes:                       16,
		MaxVertexBufferArrayStride:                2048,
		MaxInterStageShaderVariables:              16,
		MaxColorAttachments:                       8,
		MaxColorAttachmentBytesPerSample:          32,
		MaxComputeWorkgroupStorageSize:            16384,
		MaxComputeInvocationsPerWorkgroup:         256,
		MaxComputeWorkgroupSizeX:                  256,
		MaxComputeWorkgroupSizeY:                  256,
		MaxComputeWorkgroupSizeZ:                  64,
		MaxComputeWorkgroupsPerDimension:          65535,
	}
}

// checkPipelineLayout answers ErrLimitExceeded, naming the limit, for a
// pipeline layout of groups past limits: its groups, a binding number, the
// bindings of each kind a stage sees across its groups, or its dynamic
// buffers.
func (limits Limits) checkPipelineLayout(groups []BindGroupLayoutDescriptor) error {
	if err := checkLimit(len(groups), "bind groups", "MaxBindGroups", limits.MaxBindGroups); err != nil {
		return err
	}
	for groupIndex, group := range groups {
		for _, entry := range group.Entries {
			if entry.Binding >= limits.MaxBindingsPerBindGroup {
				return fmt.Errorf("%w: binding %d in group %d, MaxBindingsPerBindGroup is %d", ErrLimitExceeded, entry.Binding, groupIndex, limits.MaxBindingsPerBindGroup)
			}
		}
	}
	isUniform := func(entry BindGroupLayoutEntry) bool { return entry.Buffer.Type == BufferBindingTypeUniform }
	isStorage := func(entry BindGroupLayoutEntry) bool {
		return entry.Buffer.Type == BufferBindingTypeStorage || entry.Buffer.Type == BufferBindingTypeReadOnlyStorage
	}
	stageNames := map[ShaderStageFlags]string{
		ShaderStageFlagVertex:   " in the vertex stage",
		ShaderStageFlagFragment: " in the fragment stage",
		ShaderStageFlagCompute:  " in the compute stage",
	}
	for _, binding := range []struct {
		what      string
		limitName string
		limit     uint32
		// perStage counts the bindings each stage sees; otherwise every
		// binding counts once.
		perStage bool
		counts   func(entry BindGroupLayoutEntry) bool
	}{
		{"uniform buffers", "MaxUniformBuffersPerShaderStage", limits.MaxUniformBuffersPerShaderStage, true, isUniform},
		{"storage buffers", "MaxStorageBuffersPerShaderStage", limits.MaxStorageBuffersPerShaderStage, true, isStorage},
		{"samplers", "MaxSamplersPerShaderStage", limits.MaxSamplersPerShaderStage, true,
			func(entry BindGroupLayoutEntry) bool { return entry.Sampler.Type != SamplerBindingTypeUndefined }},
		{"sampled textures", "MaxSampledTexturesPerShaderStage", limits.MaxSampledTexturesPerShaderStage, true,
			func(entry BindGroupLayoutEntry) bool { return entry.Texture.SampleType != TextureSampleTypeUndefined }},
		{"storage textures", "MaxStorageTexturesPerShaderStage", limits.MaxStorageTexturesPerShaderStage, true,
			func(entry BindGroupLayoutEntry) bool {
				return entry.StorageTexture.Access != StorageTextureAccessUndefined
			}},
		{"dynamic uniform buffers", "MaxDynamicUniformBuffersPerPipelineLayout", limits.MaxDynamicUniformBuffersPerPipelineLayout, false,
			func(entry BindGroupLayoutEntry) bool { return entry.Buffer.HasDynamicOffset && isUniform(entry) }},
		{"dynamic storage buffers", "MaxDynamicStorageBuffersPerPipelineLayout", limits.MaxDynamicStorageBuffersPerPipelineLayout, false,
			func(entry BindGroupLayoutEntry) bool { return entry.Buffer.HasDynamicOffset && isStorage(entry) }},
	} {
		stages := []ShaderStageFlags{ShaderStageFlagNone}
		if binding.perStage {
			stages = []ShaderStageFlags{ShaderStageFlagVertex, ShaderStageFlagFragment, ShaderStageFlagCompute}
		}
		for _, stage := range stages {
			count := 0
			for _, group := range groups {
				for _, entry := range group.Entries {
					if (stage == ShaderStageFlagNone || entry.Visibility&stage != 0) && binding.counts(entry) {
						count++
					}
				}
			}
			if err := checkLimit(count, binding.what+stageNames[stage], binding.limitName, binding.limit); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkRenderPipeline answers ErrLimitExceeded, naming the limit, for a
// render pipeline of groups and buffers past limits: its layout, as
// checkPipelineLayout checks it, its vertex buffers, a stride, an
// attribute's location, its attributes across its buffers, or its groups
// and buffers together.
func (limits Limits) checkRenderPipeline(groups []BindGroupLayoutDescriptor, buffers []VertexBufferLayout) error {
	if err := limits.checkPipelineLayout(groups); err != nil {
		return err
	}
	if err := checkLimit(len(buffers), "vertex buffers", "MaxVertexBuffers", limits.MaxVertexBuffers); err != nil {
		return err
	}
	if err := checkLimit(len(groups)+len(buffers), "bind groups and vertex buffers", "MaxBindGroupsPlusVertexBuffers", limits.MaxBindGroupsPlusVertexBuffers); err != nil {
		return err
	}
	attributeCount := 0
	for bufferIndex, buffer := range buffers {
		if buffer.ArrayStride > uint64(limits.MaxVertexBufferArrayStride) {
			return fmt.Errorf("%w: vertex buffer %d's stride %d, MaxVertexBufferArrayStride is %d", ErrLimitExceeded, bufferIndex, buffer.ArrayStride, limits.MaxVertexBufferArrayStride)
		}
		for _, attribute := range buffer.Attributes {
			if attribute.ShaderLocation >= limits.MaxVertexAttributes {
				return fmt.Errorf("%w: vertex buffer %d's attribute at location %d, MaxVertexAttributes is %d", ErrLimitExceeded, bufferIndex, attribute.ShaderLocation, limits.MaxVertexAttributes)
			}
		}
		attributeCount += len(buffer.Attributes)
	}
	return checkLimit(attributeCount, "vertex attributes", "MaxVertexAttributes", limits.MaxVertexAttributes)
}

// checkLimit answers ErrLimitExceeded, naming what is counted and the limit,
// when count is past limit.
func checkLimit(count int, what, limitName string, limit uint32) error {
	if count > int(limit) {
		return fmt.Errorf("%w: %d %s, %s is %d", ErrLimitExceeded, count, what, limitName, limit)
	}
	return nil
}

// RequestAdapterOptions select the adapter gfx requests in Start.
type RequestAdapterOptions struct {
	PowerPreference PowerPreference
	// ForceFallbackAdapter asks for the software adapter: real rendering on
	// the CPU, for tests with no GPU.
	ForceFallbackAdapter bool
}

// DeviceDescriptor is what gfx requests the device with, from its Config.
type DeviceDescriptor struct {
	Label            string
	RequiredFeatures FeatureFlags
	RequiredLimits   Limits
}
