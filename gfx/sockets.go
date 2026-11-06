package gfx

import "github.com/dvoyni/cog/platform"

// InstanceSocket is the backend seam, wgpu's instance mirrored: exactly one
// plugin binds it in its Init with configurator.BindSocket[gfx.InstanceSocket],
// gogpu or the headless recording adapter, and a kernel with none panics in
// Spawn. gfx calls it from Start on.
//
// The backend keeps its native objects by gfx's handles: gfx allocates each
// handle, checks its generation and keeps its descriptor, and hands the
// backend the handle to store its object at Index. Descriptors and recording
// calls reach the backend as gfx's own, handles included, so nothing is
// translated.
type InstanceSocket interface {
	// RequestAdapter answers the adapter options select.
	RequestAdapter(options RequestAdapterOptions) (BackendAdapter, error)
	// CreateSurface creates window's surface from its native handle; gfx
	// calls it from a task pinned to the main thread.
	CreateSurface(window platform.Window, native platform.NativeHandle) error
	// ReleaseSurface releases window's surface, from a pinned task.
	ReleaseSurface(window platform.Window)
	// Release releases the instance, in gfx's Destroy.
	Release()
}

// BackendAdapter is one GPU as the backend found it.
type BackendAdapter interface {
	Info() AdapterInfo
	Features() Features
	Limits() Limits
	// RequestDevice answers the device descriptor asks for.
	RequestDevice(descriptor DeviceDescriptor) (BackendDevice, error)
	Release()
}

// BackendDevice is the backend's device and queue. Its methods are Device's,
// given the handle gfx allocated; gfx has checked every handle in a call
// before making it.
type BackendDevice interface {
	CreateBuffer(buffer Buffer, descriptor BufferDescriptor) error
	ReleaseBuffer(buffer Buffer)
	CreateTexture(texture Texture, descriptor TextureDescriptor) error
	ReleaseTexture(texture Texture)
	CreateTextureView(view TextureView, texture Texture, descriptor TextureViewDescriptor) error
	ReleaseTextureView(view TextureView)
	CreateSampler(sampler Sampler, descriptor SamplerDescriptor) error
	ReleaseSampler(sampler Sampler)
	CreateShaderModule(module ShaderModule, descriptor ShaderModuleDescriptor) error
	ReleaseShaderModule(module ShaderModule)
	CreateBindGroupLayout(layout BindGroupLayout, descriptor BindGroupLayoutDescriptor) error
	ReleaseBindGroupLayout(layout BindGroupLayout)
	CreateBindGroup(group BindGroup, descriptor BindGroupDescriptor) error
	ReleaseBindGroup(group BindGroup)
	CreatePipelineLayout(layout PipelineLayout, descriptor PipelineLayoutDescriptor) error
	ReleasePipelineLayout(layout PipelineLayout)
	// CreateRenderPipeline gets an explicit layout: gfx builds "auto"
	// layouts by reflection before calling it.
	CreateRenderPipeline(pipeline RenderPipeline, descriptor RenderPipelineDescriptor) error
	ReleaseRenderPipeline(pipeline RenderPipeline)
	CreateComputePipeline(pipeline ComputePipeline, descriptor ComputePipelineDescriptor) error
	ReleaseComputePipeline(pipeline ComputePipeline)

	WriteBuffer(buffer Buffer, offset uint64, data []byte) error
	WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error

	// CreateCommandEncoder answers an encoder; gfx wraps it to check the
	// handles recorded, and finishes it itself for a frame encoder.
	CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error)
	Submit(buffers ...CommandBuffer) error

	// SurfaceCapabilities answers what window's surface can be configured
	// with.
	SurfaceCapabilities(window platform.Window) SurfaceCapabilities
	// ConfigureSurface configures window's surface, opaque, from a pinned
	// task holding the write lock, between a present and the next acquire.
	ConfigureSurface(window platform.Window, configuration SurfaceConfiguration) error

	// AcquireFramebuffer acquires window's next framebuffer and, when status
	// is Acquired or Suboptimal, stores its view at view's index. gfx calls it
	// from a task pinned to the main thread.
	AcquireFramebuffer(window platform.Window, view TextureView) (status AcquireStatus, err error)
	// Present presents window's acquired framebuffer, from a pinned task.
	Present(window platform.Window) error

	WaitIdle() error
	Release()

	// Readback is open: https://github.com/dvoyni/cog/issues/666
	// Device-loss detection and recovery are open:
	// https://github.com/dvoyni/cog/issues/669
}
