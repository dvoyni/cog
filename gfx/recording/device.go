package recording

import (
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// device is the recording device and its queue, as adapter.RequestDevice
// answers it.
type device struct {
	instance *Instance
	// buffers and textures hold each object's descriptor at its index, under
	// the instance's mutex.
	buffers  objectTable[gfx.BufferDescriptor]
	textures objectTable[gfx.TextureDescriptor]
}

// CreateBuffer records the creation and keeps buffer at its index.
func (d *device) CreateBuffer(buffer gfx.Buffer, descriptor gfx.BufferDescriptor) error {
	return d.instance.record(CallKindCreateBuffer, CreateBufferArguments{Buffer: buffer, Descriptor: descriptor}, func() error {
		return d.buffers.store(buffer.Index(), descriptor)
	}).Error
}

// ReleaseBuffer records the release and drops buffer from its index.
func (d *device) ReleaseBuffer(buffer gfx.Buffer) {
	d.instance.record(CallKindReleaseBuffer, buffer, func() error { return d.buffers.remove(buffer.Index()) })
}

// CreateTexture records the creation, ViewFormats copied, and keeps texture
// at its index.
func (d *device) CreateTexture(texture gfx.Texture, descriptor gfx.TextureDescriptor) error {
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	return d.instance.record(CallKindCreateTexture, CreateTextureArguments{Texture: texture, Descriptor: descriptor}, func() error {
		return d.textures.store(texture.Index(), descriptor)
	}).Error
}

// ReleaseTexture records the release and drops texture from its index.
func (d *device) ReleaseTexture(texture gfx.Texture) {
	d.instance.record(CallKindReleaseTexture, texture, func() error { return d.textures.remove(texture.Index()) })
}

func (d *device) CreateTextureView(view gfx.TextureView, texture gfx.Texture, descriptor gfx.TextureViewDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseTextureView(view gfx.TextureView) { panic("gfx listing: no bodies") }

func (d *device) CreateSampler(sampler gfx.Sampler, descriptor gfx.SamplerDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseSampler(sampler gfx.Sampler) { panic("gfx listing: no bodies") }

func (d *device) CreateQuerySet(querySet gfx.QuerySet, descriptor gfx.QuerySetDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseQuerySet(querySet gfx.QuerySet) { panic("gfx listing: no bodies") }

func (d *device) TimestampPeriod() float32 { panic("gfx listing: no bodies") }

func (d *device) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseShaderModule(module gfx.ShaderModule) { panic("gfx listing: no bodies") }

func (d *device) CreateBindGroupLayout(layout gfx.BindGroupLayout, descriptor gfx.BindGroupLayoutDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseBindGroupLayout(layout gfx.BindGroupLayout) { panic("gfx listing: no bodies") }

func (d *device) CreateBindGroup(group gfx.BindGroup, descriptor gfx.BindGroupDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseBindGroup(group gfx.BindGroup) { panic("gfx listing: no bodies") }

func (d *device) CreatePipelineLayout(layout gfx.PipelineLayout, descriptor gfx.PipelineLayoutDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleasePipelineLayout(layout gfx.PipelineLayout) { panic("gfx listing: no bodies") }

func (d *device) CreateRenderPipeline(pipeline gfx.RenderPipeline, descriptor gfx.RenderPipelineDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseRenderPipeline(pipeline gfx.RenderPipeline) { panic("gfx listing: no bodies") }

func (d *device) CreateComputePipeline(pipeline gfx.ComputePipeline, descriptor gfx.ComputePipelineDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseComputePipeline(pipeline gfx.ComputePipeline) {
	panic("gfx listing: no bodies")
}

func (d *device) WriteBuffer(buffer gfx.Buffer, offset uint64, data []byte) error {
	panic("gfx listing: no bodies")
}

func (d *device) WriteTexture(destination gfx.ImageCopyTexture, data []byte, layout gfx.ImageDataLayout, size gfx.Extent3D) error {
	panic("gfx listing: no bodies")
}

func (d *device) MapBuffer(buffer gfx.Buffer, mode gfx.MapMode, offset, size uint64) error {
	panic("gfx listing: no bodies")
}

func (d *device) MapStatus(buffer gfx.Buffer) (ready bool, err error) {
	panic("gfx listing: no bodies")
}

func (d *device) MappedRange(buffer gfx.Buffer, offset, size uint64) ([]byte, error) {
	panic("gfx listing: no bodies")
}

func (d *device) UnmapBuffer(buffer gfx.Buffer) error { panic("gfx listing: no bodies") }

func (d *device) Poll() { panic("gfx listing: no bodies") }

func (d *device) CreateCommandEncoder(descriptor gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	panic("gfx listing: no bodies")
}

func (d *device) Submit(buffers ...gfx.CommandBuffer) error { panic("gfx listing: no bodies") }

func (d *device) SurfaceCapabilities(window platform.Window) gfx.SurfaceCapabilities {
	panic("gfx listing: no bodies")
}

func (d *device) ConfigureSurface(window platform.Window, configuration gfx.SurfaceConfiguration) error {
	panic("gfx listing: no bodies")
}

func (d *device) AcquireFramebuffer(window platform.Window, view gfx.TextureView) (status gfx.AcquireStatus, err error) {
	panic("gfx listing: no bodies")
}

func (d *device) Present(window platform.Window) error { panic("gfx listing: no bodies") }

// WaitIdle records the wait, and answers what AnswerNext queued for it.
func (d *device) WaitIdle() error { return d.instance.record(CallKindWaitIdle, nil, nil).Error }

// Release records the device released.
func (d *device) Release() { d.instance.record(CallKindReleaseDevice, nil, nil) }
