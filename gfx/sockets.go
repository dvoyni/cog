package gfx

import "github.com/dvoyni/cog/platform"

// InstanceSocket is the backend seam, wgpu's instance mirrored: exactly one
// plugin binds it in its Init with configurator.BindSocket[gfx.InstanceSocket],
// gogpu or the headless recording adapter, and a kernel with none panics in
// Spawn. gfx calls it from Start on, from whichever worker runs the job: it
// pins nothing, and a backend whose platform wants the surface calls on the
// main thread pins the tasks in Tasks.
//
// The backend keeps its native objects by gfx's handles: gfx allocates each
// handle, checks its generation and keeps its descriptor, and hands the
// backend the handle to store its object at Index. Descriptors and recording
// calls reach the backend as gfx's own, handles included, so nothing is
// translated.
type InstanceSocket interface {
	// RequestAdapter answers the adapter options select.
	RequestAdapter(options RequestAdapterOptions) (BackendAdapter, error)
	// CreateSurface creates window's surface from its native handle, from a
	// job holding the write lock.
	CreateSurface(window platform.Window, native platform.NativeHandle) error
	// ReleaseSurface releases window's surface, from a job holding the write
	// lock.
	ReleaseSurface(window platform.Window)
	// Release releases the instance, in gfx's Destroy.
	Release()
}

// BackendAdapter is one GPU as the backend found it.
type BackendAdapter interface {
	Info() AdapterInfo
	Features() FeatureFlags
	Limits() Limits
	// RequestDevice answers the device descriptor asks for.
	RequestDevice(descriptor DeviceDescriptor) (BackendDevice, error)
	Release()
}

// BackendDevice is the backend's device and queue. Its methods are Device's,
// given the handle gfx allocated; gfx has checked every handle in a call
// before making it. Any call answering an error answers ErrDeviceLost, as
// such, for a device loss, mapped from the backend's own errors: Vulkan's
// device-lost result, DX12's removed or reset device.
type BackendDevice interface {
	CreateBuffer(buffer Buffer, descriptor BufferDescriptor) error
	ReleaseBuffer(buffer Buffer)
	CreateTexture(texture Texture, descriptor TextureDescriptor) error
	ReleaseTexture(texture Texture)
	CreateTextureView(view TextureView, texture Texture, descriptor TextureViewDescriptor) error
	ReleaseTextureView(view TextureView)
	CreateSampler(sampler Sampler, descriptor SamplerDescriptor) error
	ReleaseSampler(sampler Sampler)
	CreateQuerySet(querySet QuerySet, descriptor QuerySetDescriptor) error
	ReleaseQuerySet(querySet QuerySet)
	TimestampPeriod() float32
	// CreateShaderModule parses the module's WGSL and reflects it once,
	// keeping the result at the module's index for the reflections of the
	// pipelines made from it; invalid WGSL answers an error.
	CreateShaderModule(module ShaderModule, descriptor ShaderModuleDescriptor) error
	ReleaseShaderModule(module ShaderModule)
	CreateBindGroupLayout(layout BindGroupLayout, descriptor BindGroupLayoutDescriptor) error
	ReleaseBindGroupLayout(layout BindGroupLayout)
	CreateBindGroup(group BindGroup, descriptor BindGroupDescriptor) error
	ReleaseBindGroup(group BindGroup)
	CreatePipelineLayout(layout PipelineLayout, descriptor PipelineLayoutDescriptor) error
	ReleasePipelineLayout(layout PipelineLayout)
	// ReflectRenderPipelineLayout answers the bind-group layouts WebGPU's
	// "auto" builds for descriptor, whose Layout is zero, from what its
	// modules' reflections keep: one for each group from 0 to the last its
	// entry points use, a group they skip answered empty, each binding
	// visible to the stages that use it. gfx creates them, and a pipeline
	// layout of them, before CreateRenderPipeline.
	ReflectRenderPipelineLayout(descriptor RenderPipelineDescriptor) ([]BindGroupLayoutDescriptor, error)
	// ReflectComputePipelineLayout is ReflectRenderPipelineLayout for a
	// compute pipeline.
	ReflectComputePipelineLayout(descriptor ComputePipelineDescriptor) ([]BindGroupLayoutDescriptor, error)
	// ReflectVertexInputs answers the inputs entryPoint of module reads at a
	// location, builtins aside, from what the module's reflection keeps; an
	// empty entryPoint is the module's one vertex entry point. gfx checks a
	// render pipeline's vertex buffers against them before
	// CreateRenderPipeline.
	ReflectVertexInputs(module ShaderModule, entryPoint string) ([]VertexInput, error)
	// CreateRenderPipeline gets an explicit layout: for an "auto" one, gfx
	// creates what ReflectRenderPipelineLayout answered before calling it.
	CreateRenderPipeline(pipeline RenderPipeline, descriptor RenderPipelineDescriptor) error
	ReleaseRenderPipeline(pipeline RenderPipeline)
	CreateComputePipeline(pipeline ComputePipeline, descriptor ComputePipelineDescriptor) error
	ReleaseComputePipeline(pipeline ComputePipeline)

	WriteBuffer(buffer Buffer, offset uint64, data []byte) error
	WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error

	// MapBuffer starts an asynchronous map; MapStatus answers whether it has
	// resolved, ready false while it waits on the GPU, or the error that
	// failed it, which ends it.
	MapBuffer(buffer Buffer, mode MapMode, offset, size uint64) error
	MapStatus(buffer Buffer) (ready bool, err error)
	MappedRange(buffer Buffer, offset, size uint64) ([]byte, error)
	UnmapBuffer(buffer Buffer) error
	// Poll resolves finished maps without waiting.
	Poll()

	// CreateCommandEncoder answers an encoder; gfx wraps it to check the
	// handles recorded, and finishes it itself for a frame encoder.
	CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error)
	Submit(buffers ...CommandBuffer) error

	// SurfaceCapabilities answers what window's surface can be configured
	// with.
	SurfaceCapabilities(window platform.Window) SurfaceCapabilities
	// ConfigureSurface configures window's surface, opaque, from a job
	// holding the write lock, between a present and the next acquire.
	ConfigureSurface(window platform.Window, configuration SurfaceConfiguration) error

	// AcquireFramebuffer acquires window's next framebuffer and, when status
	// is Acquired or Suboptimal, stores its view at view's index.
	AcquireFramebuffer(window platform.Window, view TextureView) (status AcquireStatus, err error)
	// Present presents window's acquired framebuffer.
	Present(window platform.Window) error

	WaitIdle() error
	// Release releases the device: in gfx's Destroy, and in a recovery,
	// before the adapter is asked for a new one, which gfx fills again with
	// every object under its old handle.
	Release()
}
