package recording

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// device is the recording device and its queue, as adapter.RequestDevice
// answers it.
type device struct {
	instance *Instance
	// Each table holds its type's objects, as what each was created with,
	// at their indices, under the instance's mutex.
	buffers          objectTable[gfx.BufferDescriptor]
	textures         objectTable[gfx.TextureDescriptor]
	textureViews     objectTable[CreateTextureViewArguments]
	samplers         objectTable[gfx.SamplerDescriptor]
	shaderModules    objectTable[gfx.ShaderModuleDescriptor]
	bindGroupLayouts objectTable[gfx.BindGroupLayoutDescriptor]
	bindGroups       objectTable[gfx.BindGroupDescriptor]
	pipelineLayouts  objectTable[gfx.PipelineLayoutDescriptor]
	renderPipelines  objectTable[gfx.RenderPipelineDescriptor]
	computePipelines objectTable[gfx.ComputePipelineDescriptor]
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

// CreateTextureView records the creation and keeps view at its index.
func (d *device) CreateTextureView(view gfx.TextureView, texture gfx.Texture, descriptor gfx.TextureViewDescriptor) error {
	arguments := CreateTextureViewArguments{View: view, Texture: texture, Descriptor: descriptor}
	return d.instance.record(CallKindCreateTextureView, arguments, func() error {
		return d.textureViews.store(view.Index(), arguments)
	}).Error
}

// ReleaseTextureView records the release and drops view from its index.
func (d *device) ReleaseTextureView(view gfx.TextureView) {
	d.instance.record(CallKindReleaseTextureView, view, func() error { return d.textureViews.remove(view.Index()) })
}

// CreateSampler records the creation and keeps sampler at its index.
func (d *device) CreateSampler(sampler gfx.Sampler, descriptor gfx.SamplerDescriptor) error {
	return d.instance.record(CallKindCreateSampler, CreateSamplerArguments{Sampler: sampler, Descriptor: descriptor}, func() error {
		return d.samplers.store(sampler.Index(), descriptor)
	}).Error
}

// ReleaseSampler records the release and drops sampler from its index.
func (d *device) ReleaseSampler(sampler gfx.Sampler) {
	d.instance.record(CallKindReleaseSampler, sampler, func() error { return d.samplers.remove(sampler.Index()) })
}

func (d *device) CreateQuerySet(querySet gfx.QuerySet, descriptor gfx.QuerySetDescriptor) error {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseQuerySet(querySet gfx.QuerySet) { panic("gfx listing: no bodies") }

func (d *device) TimestampPeriod() float32 { panic("gfx listing: no bodies") }

// CreateShaderModule records the creation and keeps module at its index.
func (d *device) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	return d.instance.record(CallKindCreateShaderModule, CreateShaderModuleArguments{Module: module, Descriptor: descriptor}, func() error {
		return d.shaderModules.store(module.Index(), descriptor)
	}).Error
}

// ReleaseShaderModule records the release and drops module from its index.
func (d *device) ReleaseShaderModule(module gfx.ShaderModule) {
	d.instance.record(CallKindReleaseShaderModule, module, func() error { return d.shaderModules.remove(module.Index()) })
}

// CreateBindGroupLayout records the creation and keeps layout at its index.
func (d *device) CreateBindGroupLayout(layout gfx.BindGroupLayout, descriptor gfx.BindGroupLayoutDescriptor) error {
	return d.instance.record(CallKindCreateBindGroupLayout, CreateBindGroupLayoutArguments{Layout: layout, Descriptor: descriptor}, func() error {
		return d.bindGroupLayouts.store(layout.Index(), descriptor)
	}).Error
}

// ReleaseBindGroupLayout records the release and drops layout from its index.
func (d *device) ReleaseBindGroupLayout(layout gfx.BindGroupLayout) {
	d.instance.record(CallKindReleaseBindGroupLayout, layout, func() error { return d.bindGroupLayouts.remove(layout.Index()) })
}

// CreateBindGroup records the creation and keeps group at its index.
func (d *device) CreateBindGroup(group gfx.BindGroup, descriptor gfx.BindGroupDescriptor) error {
	return d.instance.record(CallKindCreateBindGroup, CreateBindGroupArguments{Group: group, Descriptor: descriptor}, func() error {
		return d.bindGroups.store(group.Index(), descriptor)
	}).Error
}

// ReleaseBindGroup records the release and drops group from its index.
func (d *device) ReleaseBindGroup(group gfx.BindGroup) {
	d.instance.record(CallKindReleaseBindGroup, group, func() error { return d.bindGroups.remove(group.Index()) })
}

// CreatePipelineLayout records the creation and keeps layout at its index.
func (d *device) CreatePipelineLayout(layout gfx.PipelineLayout, descriptor gfx.PipelineLayoutDescriptor) error {
	return d.instance.record(CallKindCreatePipelineLayout, CreatePipelineLayoutArguments{Layout: layout, Descriptor: descriptor}, func() error {
		return d.pipelineLayouts.store(layout.Index(), descriptor)
	}).Error
}

// ReleasePipelineLayout records the release and drops layout from its index.
func (d *device) ReleasePipelineLayout(layout gfx.PipelineLayout) {
	d.instance.record(CallKindReleasePipelineLayout, layout, func() error { return d.pipelineLayouts.remove(layout.Index()) })
}

// CreateRenderPipeline records the creation and keeps pipeline at its index.
func (d *device) CreateRenderPipeline(pipeline gfx.RenderPipeline, descriptor gfx.RenderPipelineDescriptor) error {
	return d.instance.record(CallKindCreateRenderPipeline, CreateRenderPipelineArguments{Pipeline: pipeline, Descriptor: descriptor}, func() error {
		return d.renderPipelines.store(pipeline.Index(), descriptor)
	}).Error
}

// ReleaseRenderPipeline records the release and drops pipeline from its index.
func (d *device) ReleaseRenderPipeline(pipeline gfx.RenderPipeline) {
	d.instance.record(CallKindReleaseRenderPipeline, pipeline, func() error { return d.renderPipelines.remove(pipeline.Index()) })
}

// CreateComputePipeline records the creation and keeps pipeline at its index.
func (d *device) CreateComputePipeline(pipeline gfx.ComputePipeline, descriptor gfx.ComputePipelineDescriptor) error {
	return d.instance.record(CallKindCreateComputePipeline, CreateComputePipelineArguments{Pipeline: pipeline, Descriptor: descriptor}, func() error {
		return d.computePipelines.store(pipeline.Index(), descriptor)
	}).Error
}

// ReleaseComputePipeline records the release and drops pipeline from its index.
func (d *device) ReleaseComputePipeline(pipeline gfx.ComputePipeline) {
	d.instance.record(CallKindReleaseComputePipeline, pipeline, func() error { return d.computePipelines.remove(pipeline.Index()) })
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

// CreateCommandEncoder answers an encoder recording the commands given it.
func (d *device) CreateCommandEncoder(descriptor gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	return &commandEncoder{instance: d.instance}, nil
}

// Submit records the call and buffers as one submission, after the calls
// recorded before it, or answers the error AnswerNext queued for it and
// submits nothing. Every buffer is one the adapter's encoders finished.
func (d *device) Submit(buffers ...gfx.CommandBuffer) error {
	submission := Submission{Buffers: make([]CommandBuffer, len(buffers))}
	for index, buffer := range buffers {
		submission.Buffers[index] = CommandBuffer{Commands: buffer.(*commandBuffer).commands}
	}
	return d.instance.record(CallKindSubmit, nil, func() error {
		d.instance.recording.submittedAfter = append(d.instance.recording.submittedAfter, len(d.instance.recording.Calls))
		d.instance.recording.Submissions = append(d.instance.recording.Submissions, submission)
		return nil
	}).Error
}

// SurfaceCapabilities records the call and answers the Capabilities
// AnswerNext queued, or SurfaceCapabilities when none were, its slices
// copied.
func (d *device) SurfaceCapabilities(window platform.Window) gfx.SurfaceCapabilities {
	answer := d.instance.record(CallKindSurfaceCapabilities, window, nil)
	capabilities := answer.Capabilities
	if capabilities.Formats == nil && capabilities.PresentModes == nil && capabilities.AlphaModes == nil {
		capabilities = SurfaceCapabilities
	}
	return gfx.SurfaceCapabilities{
		Formats:      slices.Clone(capabilities.Formats),
		PresentModes: slices.Clone(capabilities.PresentModes),
		AlphaModes:   slices.Clone(capabilities.AlphaModes),
	}
}

// ConfigureSurface records the configure and marks window's surface
// configured; an error when window has no surface, or holds a framebuffer
// not yet presented.
func (d *device) ConfigureSurface(window platform.Window, configuration gfx.SurfaceConfiguration) error {
	return d.instance.record(CallKindConfigureSurface, ConfigureSurfaceArguments{Window: window, Configuration: configuration}, func() error {
		windowSurface, exists := d.instance.surfaces[window]
		if !exists {
			return fmt.Errorf("recording: configuring the surface of %v, which has none", window)
		}
		if windowSurface.framebuffer != (gfx.TextureView{}) {
			return fmt.Errorf("recording: configuring the surface of %v, its framebuffer not presented", window)
		}
		windowSurface.configured = true
		d.instance.surfaces[window] = windowSurface
		return nil
	}).Error
}

// AcquireFramebuffer records the acquire and answers the status AnswerNext
// queued, Acquired when none is; a status with a framebuffer keeps view as
// window's until its present. An error when window has no surface, or holds
// a framebuffer not yet presented, as wgpu answers ErrSurfaceAlreadyAcquired,
// or its surface is not configured.
func (d *device) AcquireFramebuffer(window platform.Window, view gfx.TextureView) (status gfx.AcquireStatus, err error) {
	answer := d.instance.recordAnswered(CallKindAcquireFramebuffer, AcquireFramebufferArguments{Window: window, View: view}, func(answer Answer) error {
		windowSurface, exists := d.instance.surfaces[window]
		if !exists {
			return fmt.Errorf("recording: acquiring the framebuffer of %v, which has no surface", window)
		}
		if !windowSurface.configured {
			return fmt.Errorf("recording: acquiring the framebuffer of %v, its surface not configured", window)
		}
		if windowSurface.framebuffer != (gfx.TextureView{}) {
			return fmt.Errorf("recording: acquiring the framebuffer of %v, its last not presented", window)
		}
		if status := gfx.AcquireStatus(answer.Status); status == gfx.AcquireStatusAcquired || status == gfx.AcquireStatusSuboptimal {
			windowSurface.framebuffer = view
			d.instance.surfaces[window] = windowSurface
		}
		return nil
	})
	return gfx.AcquireStatus(answer.Status), answer.Error
}

// Present records the present and drops window's framebuffer; an error when
// window has none acquired.
func (d *device) Present(window platform.Window) error {
	return d.instance.record(CallKindPresent, window, func() error {
		windowSurface, exists := d.instance.surfaces[window]
		if !exists || windowSurface.framebuffer == (gfx.TextureView{}) {
			return fmt.Errorf("recording: presenting %v, which holds no framebuffer", window)
		}
		windowSurface.framebuffer = gfx.TextureView{}
		d.instance.surfaces[window] = windowSurface
		return nil
	}).Error
}

// WaitIdle records the wait, and answers what AnswerNext queued for it.
func (d *device) WaitIdle() error { return d.instance.record(CallKindWaitIdle, nil, nil).Error }

// Release records the device released.
func (d *device) Release() { d.instance.record(CallKindReleaseDevice, nil, nil) }
