// Code generated from gputypes v0.8.0 for the listing; the numbering is gputypes', so the gogpu backend converts by cast. DO NOT EDIT.

package gfx

// DeviceType is the kind of GPU an adapter is.
type DeviceType uint8

const (
	DeviceTypeOther         DeviceType = 0x0
	DeviceTypeIntegratedGPU DeviceType = 0x1
	DeviceTypeDiscreteGPU   DeviceType = 0x2
	DeviceTypeVirtualGPU    DeviceType = 0x3
	DeviceTypeCPU           DeviceType = 0x4
)

// Backend is the native API a device runs on.
type Backend uint8

const (
	BackendEmpty         Backend = 0x0
	BackendVulkan        Backend = 0x1
	BackendMetal         Backend = 0x2
	BackendDX12          Backend = 0x3
	BackendGL            Backend = 0x4
	BackendBrowserWebGPU Backend = 0x5
)

// PowerPreference is which GPU gfx asks for when several are present.
type PowerPreference uint8

const (
	PowerPreferenceNone            PowerPreference = 0x0
	PowerPreferenceLowPower        PowerPreference = 0x1
	PowerPreferenceHighPerformance PowerPreference = 0x2
)

// StorageTextureAccess is how a shader may access a storage texture.
type StorageTextureAccess uint32

const (
	StorageTextureAccessUndefined StorageTextureAccess = 0x0
	StorageTextureAccessWriteOnly StorageTextureAccess = 0x1
	StorageTextureAccessReadOnly  StorageTextureAccess = 0x2
	StorageTextureAccessReadWrite StorageTextureAccess = 0x3
)

// BufferUsageFlags is the set of ways a buffer may be used.
type BufferUsageFlags uint64

const (
	BufferUsageFlagNone     BufferUsageFlags = 0x0
	BufferUsageFlagMapRead  BufferUsageFlags = 0x1
	BufferUsageFlagMapWrite BufferUsageFlags = 0x2
	BufferUsageFlagCopySrc  BufferUsageFlags = 0x4
	BufferUsageFlagCopyDst  BufferUsageFlags = 0x8
	BufferUsageFlagIndex    BufferUsageFlags = 0x10
	BufferUsageFlagVertex   BufferUsageFlags = 0x20
	BufferUsageFlagUniform  BufferUsageFlags = 0x40
	BufferUsageFlagStorage  BufferUsageFlags = 0x80
	BufferUsageFlagIndirect BufferUsageFlags = 0x100
)

// BufferBindingType is how a buffer is bound to a shader.
type BufferBindingType uint32

const (
	BufferBindingTypeUndefined       BufferBindingType = 0x0
	BufferBindingTypeUniform         BufferBindingType = 0x1
	BufferBindingTypeStorage         BufferBindingType = 0x2
	BufferBindingTypeReadOnlyStorage BufferBindingType = 0x3
)

// IndexFormat is the width of an index buffer's indices.
type IndexFormat uint32

const (
	IndexFormatUndefined IndexFormat = 0x0
	IndexFormatUint16    IndexFormat = 0x1
	IndexFormatUint32    IndexFormat = 0x2
)

// FeatureFlags is a set of optional device features, requested in Config and answered by Device.Features.
type FeatureFlags uint64

const (
	FeatureFlagDepthClipControl        FeatureFlags = 0x1
	FeatureFlagDepth32FloatStencil8    FeatureFlags = 0x2
	FeatureFlagTextureCompressionBC    FeatureFlags = 0x4
	FeatureFlagTextureCompressionETC2  FeatureFlags = 0x8
	FeatureFlagTextureCompressionASTC  FeatureFlags = 0x10
	FeatureFlagIndirectFirstInstance   FeatureFlags = 0x20
	FeatureFlagShaderF16               FeatureFlags = 0x40
	FeatureFlagRG11B10UfloatRenderable FeatureFlags = 0x80
	FeatureFlagBGRA8UnormStorage       FeatureFlags = 0x100
	FeatureFlagFloat32Filterable       FeatureFlags = 0x200
	FeatureFlagMultiDrawIndirect       FeatureFlags = 0x1000
	FeatureFlagShaderFloat64           FeatureFlags = 0x10000
	FeatureFlagVertexAttribute64bit    FeatureFlags = 0x20000
	FeatureFlagSubgroupOperations      FeatureFlags = 0x40000
	FeatureFlagSubgroupBarrier         FeatureFlags = 0x80000
)

// LoadOp is what an attachment holds when a render pass begins.
type LoadOp uint32

const (
	LoadOpUndefined LoadOp = 0x0
	LoadOpLoad      LoadOp = 0x1
	LoadOpClear     LoadOp = 0x2
)

// StoreOp is what is kept of an attachment when a render pass ends.
type StoreOp uint32

const (
	StoreOpUndefined StoreOp = 0x0
	StoreOpStore     StoreOp = 0x1
	StoreOpDiscard   StoreOp = 0x2
)

// BlendFactor scales a source or destination color in blending.
type BlendFactor uint32

const (
	BlendFactorUndefined         BlendFactor = 0x0
	BlendFactorZero              BlendFactor = 0x1
	BlendFactorOne               BlendFactor = 0x2
	BlendFactorSrc               BlendFactor = 0x3
	BlendFactorOneMinusSrc       BlendFactor = 0x4
	BlendFactorSrcAlpha          BlendFactor = 0x5
	BlendFactorOneMinusSrcAlpha  BlendFactor = 0x6
	BlendFactorDst               BlendFactor = 0x7
	BlendFactorOneMinusDst       BlendFactor = 0x8
	BlendFactorDstAlpha          BlendFactor = 0x9
	BlendFactorOneMinusDstAlpha  BlendFactor = 0xA
	BlendFactorSrcAlphaSaturated BlendFactor = 0xB
	BlendFactorConstant          BlendFactor = 0xC
	BlendFactorOneMinusConstant  BlendFactor = 0xD
)

// BlendOperation combines the scaled source and destination colors.
type BlendOperation uint32

const (
	BlendOperationUndefined       BlendOperation = 0x0
	BlendOperationAdd             BlendOperation = 0x1
	BlendOperationSubtract        BlendOperation = 0x2
	BlendOperationReverseSubtract BlendOperation = 0x3
	BlendOperationMin             BlendOperation = 0x4
	BlendOperationMax             BlendOperation = 0x5
)

// ColorWriteFlags is the set of color channels a render target is written in.
type ColorWriteFlags uint32

const (
	ColorWriteFlagNone  ColorWriteFlags = 0x0
	ColorWriteFlagRed   ColorWriteFlags = 0x1
	ColorWriteFlagGreen ColorWriteFlags = 0x2
	ColorWriteFlagBlue  ColorWriteFlags = 0x4
	ColorWriteFlagAlpha ColorWriteFlags = 0x8
	ColorWriteFlagAll   ColorWriteFlags = 0xF
)

// PrimitiveTopology is how vertices are assembled into primitives; its zero is the spec default, TriangleList.
type PrimitiveTopology uint32

const (
	PrimitiveTopologyTriangleList  PrimitiveTopology = 0x0
	PrimitiveTopologyPointList     PrimitiveTopology = 0x1
	PrimitiveTopologyLineList      PrimitiveTopology = 0x2
	PrimitiveTopologyLineStrip     PrimitiveTopology = 0x3
	PrimitiveTopologyTriangleStrip PrimitiveTopology = 0x4
)

// FrontFace is the winding of a front-facing triangle; its zero is the spec default, CCW.
type FrontFace uint32

const (
	FrontFaceCCW FrontFace = 0x0
	FrontFaceCW  FrontFace = 0x1
)

// CullMode is which triangles are discarded by facing; its zero is the spec default, none.
type CullMode uint32

const (
	CullModeNone  CullMode = 0x0
	CullModeFront CullMode = 0x1
	CullModeBack  CullMode = 0x2
)

// StencilOperation is what a stencil test does to the stencil value.
type StencilOperation uint32

const (
	StencilOperationUndefined      StencilOperation = 0x0
	StencilOperationKeep           StencilOperation = 0x1
	StencilOperationZero           StencilOperation = 0x2
	StencilOperationReplace        StencilOperation = 0x3
	StencilOperationInvert         StencilOperation = 0x4
	StencilOperationIncrementClamp StencilOperation = 0x5
	StencilOperationDecrementClamp StencilOperation = 0x6
	StencilOperationIncrementWrap  StencilOperation = 0x7
	StencilOperationDecrementWrap  StencilOperation = 0x8
)

// AddressMode is how a sampler reads coordinates outside a texture.
type AddressMode uint32

const (
	AddressModeUndefined    AddressMode = 0x0
	AddressModeClampToEdge  AddressMode = 0x1
	AddressModeRepeat       AddressMode = 0x2
	AddressModeMirrorRepeat AddressMode = 0x3
)

// FilterMode is how a sampler filters between texels or mip levels.
type FilterMode uint32

const (
	FilterModeUndefined FilterMode = 0x0
	FilterModeNearest   FilterMode = 0x1
	FilterModeLinear    FilterMode = 0x2
)

// CompareFunction is how a depth, stencil or comparison-sampler test compares two values.
type CompareFunction uint32

const (
	CompareFunctionUndefined    CompareFunction = 0x0
	CompareFunctionNever        CompareFunction = 0x1
	CompareFunctionLess         CompareFunction = 0x2
	CompareFunctionEqual        CompareFunction = 0x3
	CompareFunctionLessEqual    CompareFunction = 0x4
	CompareFunctionGreater      CompareFunction = 0x5
	CompareFunctionNotEqual     CompareFunction = 0x6
	CompareFunctionGreaterEqual CompareFunction = 0x7
	CompareFunctionAlways       CompareFunction = 0x8
)

// SamplerBindingType is how a sampler is bound to a shader.
type SamplerBindingType uint32

const (
	SamplerBindingTypeUndefined    SamplerBindingType = 0x0
	SamplerBindingTypeFiltering    SamplerBindingType = 0x1
	SamplerBindingTypeNonFiltering SamplerBindingType = 0x2
	SamplerBindingTypeComparison   SamplerBindingType = 0x3
)

// ShaderStageFlags is a set of shader stages, the visibility of a binding.
type ShaderStageFlags uint32

const (
	ShaderStageFlagNone     ShaderStageFlags = 0x0
	ShaderStageFlagVertex   ShaderStageFlags = 0x1
	ShaderStageFlagFragment ShaderStageFlags = 0x2
	ShaderStageFlagCompute  ShaderStageFlags = 0x4
)

// TextureFormat is the layout of a texture's texels.
type TextureFormat uint32

const (
	TextureFormatUndefined            TextureFormat = 0x0
	TextureFormatR8Unorm              TextureFormat = 0x1
	TextureFormatR8Snorm              TextureFormat = 0x2
	TextureFormatR8Uint               TextureFormat = 0x3
	TextureFormatR8Sint               TextureFormat = 0x4
	TextureFormatR16Unorm             TextureFormat = 0x5
	TextureFormatR16Snorm             TextureFormat = 0x6
	TextureFormatR16Uint              TextureFormat = 0x7
	TextureFormatR16Sint              TextureFormat = 0x8
	TextureFormatR16Float             TextureFormat = 0x9
	TextureFormatRG8Unorm             TextureFormat = 0xA
	TextureFormatRG8Snorm             TextureFormat = 0xB
	TextureFormatRG8Uint              TextureFormat = 0xC
	TextureFormatRG8Sint              TextureFormat = 0xD
	TextureFormatR32Float             TextureFormat = 0xE
	TextureFormatR32Uint              TextureFormat = 0xF
	TextureFormatR32Sint              TextureFormat = 0x10
	TextureFormatRG16Unorm            TextureFormat = 0x11
	TextureFormatRG16Snorm            TextureFormat = 0x12
	TextureFormatRG16Uint             TextureFormat = 0x13
	TextureFormatRG16Sint             TextureFormat = 0x14
	TextureFormatRG16Float            TextureFormat = 0x15
	TextureFormatRGBA8Unorm           TextureFormat = 0x16
	TextureFormatRGBA8UnormSrgb       TextureFormat = 0x17
	TextureFormatRGBA8Snorm           TextureFormat = 0x18
	TextureFormatRGBA8Uint            TextureFormat = 0x19
	TextureFormatRGBA8Sint            TextureFormat = 0x1A
	TextureFormatBGRA8Unorm           TextureFormat = 0x1B
	TextureFormatBGRA8UnormSrgb       TextureFormat = 0x1C
	TextureFormatRGB10A2Uint          TextureFormat = 0x1D
	TextureFormatRGB10A2Unorm         TextureFormat = 0x1E
	TextureFormatRG11B10Ufloat        TextureFormat = 0x1F
	TextureFormatRGB9E5Ufloat         TextureFormat = 0x20
	TextureFormatRG32Float            TextureFormat = 0x21
	TextureFormatRG32Uint             TextureFormat = 0x22
	TextureFormatRG32Sint             TextureFormat = 0x23
	TextureFormatRGBA16Unorm          TextureFormat = 0x24
	TextureFormatRGBA16Snorm          TextureFormat = 0x25
	TextureFormatRGBA16Uint           TextureFormat = 0x26
	TextureFormatRGBA16Sint           TextureFormat = 0x27
	TextureFormatRGBA16Float          TextureFormat = 0x28
	TextureFormatRGBA32Float          TextureFormat = 0x29
	TextureFormatRGBA32Uint           TextureFormat = 0x2A
	TextureFormatRGBA32Sint           TextureFormat = 0x2B
	TextureFormatStencil8             TextureFormat = 0x2C
	TextureFormatDepth16Unorm         TextureFormat = 0x2D
	TextureFormatDepth24Plus          TextureFormat = 0x2E
	TextureFormatDepth24PlusStencil8  TextureFormat = 0x2F
	TextureFormatDepth32Float         TextureFormat = 0x30
	TextureFormatDepth32FloatStencil8 TextureFormat = 0x31
	TextureFormatBC1RGBAUnorm         TextureFormat = 0x32
	TextureFormatBC1RGBAUnormSrgb     TextureFormat = 0x33
	TextureFormatBC2RGBAUnorm         TextureFormat = 0x34
	TextureFormatBC2RGBAUnormSrgb     TextureFormat = 0x35
	TextureFormatBC3RGBAUnorm         TextureFormat = 0x36
	TextureFormatBC3RGBAUnormSrgb     TextureFormat = 0x37
	TextureFormatBC4RUnorm            TextureFormat = 0x38
	TextureFormatBC4RSnorm            TextureFormat = 0x39
	TextureFormatBC5RGUnorm           TextureFormat = 0x3A
	TextureFormatBC5RGSnorm           TextureFormat = 0x3B
	TextureFormatBC6HRGBUfloat        TextureFormat = 0x3C
	TextureFormatBC6HRGBFloat         TextureFormat = 0x3D
	TextureFormatBC7RGBAUnorm         TextureFormat = 0x3E
	TextureFormatBC7RGBAUnormSrgb     TextureFormat = 0x3F
	TextureFormatETC2RGB8Unorm        TextureFormat = 0x40
	TextureFormatETC2RGB8UnormSrgb    TextureFormat = 0x41
	TextureFormatETC2RGB8A1Unorm      TextureFormat = 0x42
	TextureFormatETC2RGB8A1UnormSrgb  TextureFormat = 0x43
	TextureFormatETC2RGBA8Unorm       TextureFormat = 0x44
	TextureFormatETC2RGBA8UnormSrgb   TextureFormat = 0x45
	TextureFormatEACR11Unorm          TextureFormat = 0x46
	TextureFormatEACR11Snorm          TextureFormat = 0x47
	TextureFormatEACRG11Unorm         TextureFormat = 0x48
	TextureFormatEACRG11Snorm         TextureFormat = 0x49
	TextureFormatASTC4x4Unorm         TextureFormat = 0x4A
	TextureFormatASTC4x4UnormSrgb     TextureFormat = 0x4B
	TextureFormatASTC5x4Unorm         TextureFormat = 0x4C
	TextureFormatASTC5x4UnormSrgb     TextureFormat = 0x4D
	TextureFormatASTC5x5Unorm         TextureFormat = 0x4E
	TextureFormatASTC5x5UnormSrgb     TextureFormat = 0x4F
	TextureFormatASTC6x5Unorm         TextureFormat = 0x50
	TextureFormatASTC6x5UnormSrgb     TextureFormat = 0x51
	TextureFormatASTC6x6Unorm         TextureFormat = 0x52
	TextureFormatASTC6x6UnormSrgb     TextureFormat = 0x53
	TextureFormatASTC8x5Unorm         TextureFormat = 0x54
	TextureFormatASTC8x5UnormSrgb     TextureFormat = 0x55
	TextureFormatASTC8x6Unorm         TextureFormat = 0x56
	TextureFormatASTC8x6UnormSrgb     TextureFormat = 0x57
	TextureFormatASTC8x8Unorm         TextureFormat = 0x58
	TextureFormatASTC8x8UnormSrgb     TextureFormat = 0x59
	TextureFormatASTC10x5Unorm        TextureFormat = 0x5A
	TextureFormatASTC10x5UnormSrgb    TextureFormat = 0x5B
	TextureFormatASTC10x6Unorm        TextureFormat = 0x5C
	TextureFormatASTC10x6UnormSrgb    TextureFormat = 0x5D
	TextureFormatASTC10x8Unorm        TextureFormat = 0x5E
	TextureFormatASTC10x8UnormSrgb    TextureFormat = 0x5F
	TextureFormatASTC10x10Unorm       TextureFormat = 0x60
	TextureFormatASTC10x10UnormSrgb   TextureFormat = 0x61
	TextureFormatASTC12x10Unorm       TextureFormat = 0x62
	TextureFormatASTC12x10UnormSrgb   TextureFormat = 0x63
	TextureFormatASTC12x12Unorm       TextureFormat = 0x64
	TextureFormatASTC12x12UnormSrgb   TextureFormat = 0x65
)

// TextureDimension is a texture's dimensionality.
type TextureDimension uint32

const (
	TextureDimensionUndefined TextureDimension = 0x0
	TextureDimension1D        TextureDimension = 0x1
	TextureDimension2D        TextureDimension = 0x2
	TextureDimension3D        TextureDimension = 0x3
)

// TextureViewDimension is how a view presents its texture to a shader.
type TextureViewDimension uint32

const (
	TextureViewDimensionUndefined TextureViewDimension = 0x0
	TextureViewDimension1D        TextureViewDimension = 0x1
	TextureViewDimension2D        TextureViewDimension = 0x2
	TextureViewDimension2DArray   TextureViewDimension = 0x3
	TextureViewDimensionCube      TextureViewDimension = 0x4
	TextureViewDimensionCubeArray TextureViewDimension = 0x5
	TextureViewDimension3D        TextureViewDimension = 0x6
)

// TextureAspect is which aspects of a depth-stencil texture a view or copy covers.
type TextureAspect uint32

const (
	TextureAspectUndefined   TextureAspect = 0x0
	TextureAspectAll         TextureAspect = 0x1
	TextureAspectStencilOnly TextureAspect = 0x2
	TextureAspectDepthOnly   TextureAspect = 0x3
)

// TextureUsageFlags is the set of ways a texture may be used.
type TextureUsageFlags uint64

const (
	TextureUsageFlagNone             TextureUsageFlags = 0x0
	TextureUsageFlagCopySrc          TextureUsageFlags = 0x1
	TextureUsageFlagCopyDst          TextureUsageFlags = 0x2
	TextureUsageFlagTextureBinding   TextureUsageFlags = 0x4
	TextureUsageFlagStorageBinding   TextureUsageFlags = 0x8
	TextureUsageFlagRenderAttachment TextureUsageFlags = 0x10
)

// TextureSampleType is what a shader samples from a texture binding.
type TextureSampleType uint32

const (
	TextureSampleTypeUndefined         TextureSampleType = 0x0
	TextureSampleTypeFloat             TextureSampleType = 0x1
	TextureSampleTypeUnfilterableFloat TextureSampleType = 0x2
	TextureSampleTypeDepth             TextureSampleType = 0x3
	TextureSampleTypeSint              TextureSampleType = 0x4
	TextureSampleTypeUint              TextureSampleType = 0x5
)

// VertexFormat is the layout of one vertex attribute.
type VertexFormat uint32

const (
	VertexFormatUndefined    VertexFormat = 0x0
	VertexFormatUint8x2      VertexFormat = 0x1
	VertexFormatUint8x4      VertexFormat = 0x2
	VertexFormatSint8x2      VertexFormat = 0x3
	VertexFormatSint8x4      VertexFormat = 0x4
	VertexFormatUnorm8x2     VertexFormat = 0x5
	VertexFormatUnorm8x4     VertexFormat = 0x6
	VertexFormatSnorm8x2     VertexFormat = 0x7
	VertexFormatSnorm8x4     VertexFormat = 0x8
	VertexFormatUint16x2     VertexFormat = 0x9
	VertexFormatUint16x4     VertexFormat = 0xA
	VertexFormatSint16x2     VertexFormat = 0xB
	VertexFormatSint16x4     VertexFormat = 0xC
	VertexFormatUnorm16x2    VertexFormat = 0xD
	VertexFormatUnorm16x4    VertexFormat = 0xE
	VertexFormatSnorm16x2    VertexFormat = 0xF
	VertexFormatSnorm16x4    VertexFormat = 0x10
	VertexFormatFloat16x2    VertexFormat = 0x11
	VertexFormatFloat16x4    VertexFormat = 0x12
	VertexFormatFloat32      VertexFormat = 0x13
	VertexFormatFloat32x2    VertexFormat = 0x14
	VertexFormatFloat32x3    VertexFormat = 0x15
	VertexFormatFloat32x4    VertexFormat = 0x16
	VertexFormatUint32       VertexFormat = 0x17
	VertexFormatUint32x2     VertexFormat = 0x18
	VertexFormatUint32x3     VertexFormat = 0x19
	VertexFormatUint32x4     VertexFormat = 0x1A
	VertexFormatSint32       VertexFormat = 0x1B
	VertexFormatSint32x2     VertexFormat = 0x1C
	VertexFormatSint32x3     VertexFormat = 0x1D
	VertexFormatSint32x4     VertexFormat = 0x1E
	VertexFormatUnorm1010102 VertexFormat = 0x1F
)

// VertexStepMode is whether a vertex buffer advances per vertex or per instance.
type VertexStepMode uint32

const (
	VertexStepModeUndefined           VertexStepMode = 0x0
	VertexStepModeVertexBufferNotUsed VertexStepMode = 0x1
	VertexStepModeVertex              VertexStepMode = 0x2
	VertexStepModeInstance            VertexStepMode = 0x3
)
