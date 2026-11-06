package gfx

import (
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// device is the Device resource's value: installed empty in Init and filled
// in Start, before any job reads it. It has no exclusive operation, so a job
// holding the read lock cannot assert it into an ExclusiveDevice.
type device struct {
	backend     BackendDevice
	features    FeatureFlags
	limits      Limits
	adapterInfo AdapterInfo
	// logger is the kernel's, for what a call answers no error for.
	logger   *kernel.Logger
	buffers  table[Buffer, BufferDescriptor]
	textures table[Texture, TextureDescriptor]
}

func (d *device) Features() FeatureFlags { return d.features }

func (d *device) Limits() Limits { return d.limits }

func (d *device) AdapterInfo() AdapterInfo { return d.adapterInfo }

func (d *device) CreateBuffer(descriptor BufferDescriptor) (Buffer, error) {
	buffer := d.buffers.acquire(descriptor)
	if err := d.backend.CreateBuffer(buffer, descriptor); err != nil {
		_ = d.buffers.retire(buffer)
		d.buffers.release(buffer)
		return Buffer{}, err
	}
	return buffer, nil
}

func (d *device) ReleaseBuffer(buffer Buffer) {
	if err := d.buffers.retire(buffer); err != nil {
		d.logger.LogErr(err)
		return
	}
	d.backend.ReleaseBuffer(buffer)
	d.buffers.release(buffer)
}

func (d *device) BufferDescriptor(buffer Buffer) BufferDescriptor {
	descriptor, _ := d.buffers.descriptor(buffer)
	return descriptor
}

func (d *device) CreateTexture(descriptor TextureDescriptor) (Texture, error) {
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	texture := d.textures.acquire(descriptor)
	if err := d.backend.CreateTexture(texture, descriptor); err != nil {
		_ = d.textures.retire(texture)
		d.textures.release(texture)
		return Texture{}, err
	}
	return texture, nil
}

func (d *device) ReleaseTexture(texture Texture) {
	if err := d.textures.retire(texture); err != nil {
		d.logger.LogErr(err)
		return
	}
	d.backend.ReleaseTexture(texture)
	d.textures.release(texture)
}

func (d *device) TextureDescriptor(texture Texture) TextureDescriptor {
	descriptor, _ := d.textures.descriptor(texture)
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	return descriptor
}

func (d *device) CreateTextureView(texture Texture, descriptor TextureViewDescriptor) (TextureView, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseTextureView(view TextureView) { panic("gfx listing: no bodies") }

func (d *device) TextureViewTexture(view TextureView) Texture { panic("gfx listing: no bodies") }

func (d *device) CreateSampler(descriptor SamplerDescriptor) (Sampler, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseSampler(sampler Sampler) { panic("gfx listing: no bodies") }

func (d *device) CreateQuerySet(descriptor QuerySetDescriptor) (QuerySet, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseQuerySet(querySet QuerySet) { panic("gfx listing: no bodies") }

func (d *device) TimestampPeriod() float32 { panic("gfx listing: no bodies") }

func (d *device) CreateShaderModule(descriptor ShaderModuleDescriptor) (ShaderModule, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseShaderModule(module ShaderModule) { panic("gfx listing: no bodies") }

func (d *device) CreateBindGroupLayout(descriptor BindGroupLayoutDescriptor) (BindGroupLayout, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseBindGroupLayout(layout BindGroupLayout) { panic("gfx listing: no bodies") }

func (d *device) CreateBindGroup(descriptor BindGroupDescriptor) (BindGroup, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseBindGroup(group BindGroup) { panic("gfx listing: no bodies") }

func (d *device) CreatePipelineLayout(descriptor PipelineLayoutDescriptor) (PipelineLayout, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleasePipelineLayout(layout PipelineLayout) { panic("gfx listing: no bodies") }

func (d *device) CreateRenderPipeline(descriptor RenderPipelineDescriptor) (RenderPipeline, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseRenderPipeline(pipeline RenderPipeline) { panic("gfx listing: no bodies") }

func (d *device) RenderPipelineBindGroupLayout(pipeline RenderPipeline, group uint32) BindGroupLayout {
	panic("gfx listing: no bodies")
}

func (d *device) CreateComputePipeline(descriptor ComputePipelineDescriptor) (ComputePipeline, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseComputePipeline(pipeline ComputePipeline) { panic("gfx listing: no bodies") }

func (d *device) ComputePipelineBindGroupLayout(pipeline ComputePipeline, group uint32) BindGroupLayout {
	panic("gfx listing: no bodies")
}

func (d *device) WriteBuffer(buffer Buffer, offset uint64, data []byte) error {
	panic("gfx listing: no bodies")
}

func (d *device) WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error {
	panic("gfx listing: no bodies")
}

func (d *device) Encoder(frame Frame, order SubmitOrder) (CommandEncoder, error) {
	panic("gfx listing: no bodies")
}

func (d *device) Framebuffer(frame Frame, window platform.Window) TextureView {
	panic("gfx listing: no bodies")
}

func (d *device) SurfaceConfiguration(window platform.Window) (SurfaceConfiguration, bool) {
	panic("gfx listing: no bodies")
}

func (d *device) CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error) {
	panic("gfx listing: no bodies")
}

func (d *device) Submit(buffers ...CommandBuffer) error { panic("gfx listing: no bodies") }

func (d *device) MapBuffer(buffer Buffer, mode MapMode, offset, size uint64) error {
	panic("gfx listing: no bodies")
}

func (d *device) MappedRange(buffer Buffer, offset, size uint64) ([]byte, error) {
	panic("gfx listing: no bodies")
}

func (d *device) UnmapBuffer(buffer Buffer) error { panic("gfx listing: no bodies") }

func (d *device) Poll() { panic("gfx listing: no bodies") }

func (d *device) RequestCapture(window platform.Window) error { panic("gfx listing: no bodies") }

// exclusiveDevice is the device as ElevateDeviceAccess answers it, with the
// operations only the write lock may reach.
type exclusiveDevice struct {
	*device
}

func (d exclusiveDevice) WaitIdle() error { return d.backend.WaitIdle() }

func (d exclusiveDevice) RequestSurface(request SurfaceRequest) { panic("gfx listing: no bodies") }
