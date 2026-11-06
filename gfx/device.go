package gfx

import (
	"fmt"
	"maps"
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// device is the Device resource's value: installed empty in Init and filled
// in Start, before any job reads it. It has no exclusive operation, so a job
// holding the read lock cannot assert it into an ExclusiveDevice.
type device struct {
	instance    InstanceSocket
	backend     BackendDevice
	features    FeatureFlags
	limits      Limits
	adapterInfo AdapterInfo
	// logger is the kernel's, for what a call answers no error for.
	logger *kernel.Logger
	// surfaces are the windows with a surface, in the order they got it;
	// only jobs holding the write lock change it.
	surfaces []platform.Window
	// surfaceRequest is what every surface is configured from.
	surfaceRequest SurfaceRequest
	// configurations are what each window's surface is configured with;
	// only jobs holding the write lock change them.
	configurations map[platform.Window]SurfaceConfiguration
	frames         framePool
	// Each table holds its type's handles with what each was created with,
	// slices copied, which a recovery creates it again from.
	buffers          table[Buffer, BufferDescriptor]
	textures         table[Texture, TextureDescriptor]
	textureViews     table[TextureView, textureViewSource]
	samplers         table[Sampler, SamplerDescriptor]
	shaderModules    table[ShaderModule, ShaderModuleDescriptor]
	bindGroupLayouts table[BindGroupLayout, BindGroupLayoutDescriptor]
	bindGroups       table[BindGroup, BindGroupDescriptor]
	pipelineLayouts  table[PipelineLayout, PipelineLayoutDescriptor]
	renderPipelines  table[RenderPipeline, pipelineSource[RenderPipelineDescriptor]]
	computePipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
}

func (d *device) Features() FeatureFlags { return d.features }

func (d *device) Limits() Limits { return d.limits }

func (d *device) AdapterInfo() AdapterInfo { return d.adapterInfo }

func (d *device) CreateBuffer(descriptor BufferDescriptor) (Buffer, error) {
	return createResource(&d.buffers, descriptor, d.backend.CreateBuffer)
}

func (d *device) ReleaseBuffer(buffer Buffer) {
	releaseResource(d.logger, &d.buffers, buffer, d.backend.ReleaseBuffer)
}

func (d *device) BufferDescriptor(buffer Buffer) BufferDescriptor {
	descriptor, _ := d.buffers.descriptor(buffer)
	return descriptor
}

func (d *device) CreateTexture(descriptor TextureDescriptor) (Texture, error) {
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	return createResource(&d.textures, descriptor, d.backend.CreateTexture)
}

func (d *device) ReleaseTexture(texture Texture) {
	releaseResource(d.logger, &d.textures, texture, d.backend.ReleaseTexture)
}

func (d *device) TextureDescriptor(texture Texture) TextureDescriptor {
	descriptor, _ := d.textures.descriptor(texture)
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	return descriptor
}

func (d *device) CreateTextureView(texture Texture, descriptor TextureViewDescriptor) (TextureView, error) {
	if err := d.textures.check(texture); err != nil {
		return TextureView{}, fmt.Errorf("%w: the view's texture", err)
	}
	source := textureViewSource{texture: texture, descriptor: descriptor}
	return createResource(&d.textureViews, source, func(view TextureView, source textureViewSource) error {
		return d.backend.CreateTextureView(view, source.texture, source.descriptor)
	})
}

func (d *device) ReleaseTextureView(view TextureView) {
	releaseResource(d.logger, &d.textureViews, view, d.backend.ReleaseTextureView)
}

func (d *device) TextureViewTexture(view TextureView) Texture {
	source, _ := d.textureViews.descriptor(view)
	return source.texture
}

func (d *device) CreateSampler(descriptor SamplerDescriptor) (Sampler, error) {
	return createResource(&d.samplers, descriptor, d.backend.CreateSampler)
}

func (d *device) ReleaseSampler(sampler Sampler) {
	releaseResource(d.logger, &d.samplers, sampler, d.backend.ReleaseSampler)
}

func (d *device) CreateQuerySet(descriptor QuerySetDescriptor) (QuerySet, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseQuerySet(querySet QuerySet) { panic("gfx listing: no bodies") }

func (d *device) TimestampPeriod() float32 { panic("gfx listing: no bodies") }

func (d *device) CreateShaderModule(descriptor ShaderModuleDescriptor) (ShaderModule, error) {
	return createResource(&d.shaderModules, descriptor, d.backend.CreateShaderModule)
}

func (d *device) ReleaseShaderModule(module ShaderModule) {
	releaseResource(d.logger, &d.shaderModules, module, d.backend.ReleaseShaderModule)
}

func (d *device) CreateBindGroupLayout(descriptor BindGroupLayoutDescriptor) (BindGroupLayout, error) {
	descriptor.Entries = slices.Clone(descriptor.Entries)
	return createResource(&d.bindGroupLayouts, descriptor, d.backend.CreateBindGroupLayout)
}

func (d *device) ReleaseBindGroupLayout(layout BindGroupLayout) {
	releaseResource(d.logger, &d.bindGroupLayouts, layout, d.backend.ReleaseBindGroupLayout)
}

func (d *device) CreateBindGroup(descriptor BindGroupDescriptor) (BindGroup, error) {
	if err := d.bindGroupLayouts.check(descriptor.Layout); err != nil {
		return BindGroup{}, fmt.Errorf("%w: the bind group's layout", err)
	}
	for _, entry := range descriptor.Entries {
		if entry.Buffer != (Buffer{}) && d.buffers.check(entry.Buffer) != nil {
			return BindGroup{}, fmt.Errorf("%w: the buffer at binding %d", ErrReleased, entry.Binding)
		}
		if entry.Sampler != (Sampler{}) && d.samplers.check(entry.Sampler) != nil {
			return BindGroup{}, fmt.Errorf("%w: the sampler at binding %d", ErrReleased, entry.Binding)
		}
		if entry.TextureView != (TextureView{}) && d.textureViews.check(entry.TextureView) != nil {
			return BindGroup{}, fmt.Errorf("%w: the texture view at binding %d", ErrReleased, entry.Binding)
		}
	}
	descriptor.Entries = slices.Clone(descriptor.Entries)
	return createResource(&d.bindGroups, descriptor, d.backend.CreateBindGroup)
}

func (d *device) ReleaseBindGroup(group BindGroup) {
	releaseResource(d.logger, &d.bindGroups, group, d.backend.ReleaseBindGroup)
}

func (d *device) CreatePipelineLayout(descriptor PipelineLayoutDescriptor) (PipelineLayout, error) {
	for group, layout := range descriptor.BindGroupLayouts {
		if err := d.bindGroupLayouts.check(layout); err != nil {
			return PipelineLayout{}, fmt.Errorf("%w: the layout of group %d", err, group)
		}
	}
	descriptor.BindGroupLayouts = slices.Clone(descriptor.BindGroupLayouts)
	return createResource(&d.pipelineLayouts, descriptor, d.backend.CreatePipelineLayout)
}

func (d *device) ReleasePipelineLayout(layout PipelineLayout) {
	releaseResource(d.logger, &d.pipelineLayouts, layout, d.backend.ReleasePipelineLayout)
}

func (d *device) CreateRenderPipeline(descriptor RenderPipelineDescriptor) (RenderPipeline, error) {
	if descriptor.Layout != (PipelineLayout{}) {
		if err := d.pipelineLayouts.check(descriptor.Layout); err != nil {
			return RenderPipeline{}, fmt.Errorf("%w: the pipeline's layout", err)
		}
	}
	if err := d.shaderModules.check(descriptor.Vertex.Module); err != nil {
		return RenderPipeline{}, fmt.Errorf("%w: the vertex module", err)
	}
	if descriptor.Fragment.Module != (ShaderModule{}) && d.shaderModules.check(descriptor.Fragment.Module) != nil {
		return RenderPipeline{}, fmt.Errorf("%w: the fragment module", ErrReleased)
	}
	inputs, err := d.backend.ReflectVertexInputs(descriptor.Vertex.Module, descriptor.Vertex.EntryPoint)
	if err != nil {
		return RenderPipeline{}, err
	}
	if err := checkVertexInputs(descriptor.Vertex, inputs); err != nil {
		return RenderPipeline{}, err
	}
	source := pipelineSource[RenderPipelineDescriptor]{descriptor: descriptor.clone()}
	autoLayout := source.descriptor.Layout == (PipelineLayout{})
	groups := d.groupDescriptors(source.descriptor.Layout)
	if autoLayout {
		if groups, err = d.backend.ReflectRenderPipelineLayout(source.descriptor); err != nil {
			return RenderPipeline{}, err
		}
	}
	if err := d.limits.checkRenderPipeline(groups, source.descriptor.Vertex.Buffers); err != nil {
		return RenderPipeline{}, err
	}
	if autoLayout {
		if source.descriptor.Layout, err = d.createAutoLayout(groups); err != nil {
			return RenderPipeline{}, err
		}
		source.autoLayout = true
	}
	pipeline, err := createResource(&d.renderPipelines, source, func(pipeline RenderPipeline, source pipelineSource[RenderPipelineDescriptor]) error {
		return d.backend.CreateRenderPipeline(pipeline, source.descriptor)
	})
	if err != nil && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
	return pipeline, err
}

func (d *device) ReleaseRenderPipeline(pipeline RenderPipeline) {
	source, _ := d.renderPipelines.descriptor(pipeline)
	releaseResource(d.logger, &d.renderPipelines, pipeline, func(pipeline RenderPipeline) {
		d.backend.ReleaseRenderPipeline(pipeline)
		if source.autoLayout {
			d.releaseAutoLayout(source.descriptor.Layout)
		}
	})
}

func (d *device) RenderPipelineBindGroupLayout(pipeline RenderPipeline, group uint32) BindGroupLayout {
	source, _ := d.renderPipelines.descriptor(pipeline)
	return d.groupLayout(source.descriptor.Layout, group)
}

func (d *device) CreateComputePipeline(descriptor ComputePipelineDescriptor) (ComputePipeline, error) {
	if descriptor.Layout != (PipelineLayout{}) {
		if err := d.pipelineLayouts.check(descriptor.Layout); err != nil {
			return ComputePipeline{}, fmt.Errorf("%w: the pipeline's layout", err)
		}
	}
	if err := d.shaderModules.check(descriptor.Module); err != nil {
		return ComputePipeline{}, fmt.Errorf("%w: the module", err)
	}
	descriptor.Constants = maps.Clone(descriptor.Constants)
	source := pipelineSource[ComputePipelineDescriptor]{descriptor: descriptor}
	autoLayout := source.descriptor.Layout == (PipelineLayout{})
	groups := d.groupDescriptors(source.descriptor.Layout)
	var err error
	if autoLayout {
		if groups, err = d.backend.ReflectComputePipelineLayout(source.descriptor); err != nil {
			return ComputePipeline{}, err
		}
	}
	if err := d.limits.checkPipelineLayout(groups); err != nil {
		return ComputePipeline{}, err
	}
	if autoLayout {
		if source.descriptor.Layout, err = d.createAutoLayout(groups); err != nil {
			return ComputePipeline{}, err
		}
		source.autoLayout = true
	}
	pipeline, err := createResource(&d.computePipelines, source, func(pipeline ComputePipeline, source pipelineSource[ComputePipelineDescriptor]) error {
		return d.backend.CreateComputePipeline(pipeline, source.descriptor)
	})
	if err != nil && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
	return pipeline, err
}

func (d *device) ReleaseComputePipeline(pipeline ComputePipeline) {
	source, _ := d.computePipelines.descriptor(pipeline)
	releaseResource(d.logger, &d.computePipelines, pipeline, func(pipeline ComputePipeline) {
		d.backend.ReleaseComputePipeline(pipeline)
		if source.autoLayout {
			d.releaseAutoLayout(source.descriptor.Layout)
		}
	})
}

func (d *device) ComputePipelineBindGroupLayout(pipeline ComputePipeline, group uint32) BindGroupLayout {
	source, _ := d.computePipelines.descriptor(pipeline)
	return d.groupLayout(source.descriptor.Layout, group)
}

func (d *device) WriteBuffer(buffer Buffer, offset uint64, data []byte) error {
	if err := d.buffers.check(buffer); err != nil {
		return err
	}
	return d.backend.WriteBuffer(buffer, offset, data)
}

func (d *device) WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error {
	if err := d.textures.check(destination.Texture); err != nil {
		return err
	}
	return d.backend.WriteTexture(destination, data, layout, size)
}

func (d *device) Encoder(frame Frame, order SubmitOrder) (CommandEncoder, error) {
	encoder, err := d.backend.CreateCommandEncoder(CommandEncoderDescriptor{})
	if err != nil {
		return discardEncoder{}, err
	}
	if err := d.frames.addEncoder(frame, frameEncoder{order: order, encoder: encoder}); err != nil {
		encoder.DiscardEncoding()
		return discardEncoder{}, err
	}
	return encoder, nil
}

func (d *device) Framebuffer(frame Frame, window platform.Window) TextureView {
	return d.frames.framebuffer(frame, window)
}

func (d *device) SurfaceConfiguration(window platform.Window) (SurfaceConfiguration, bool) {
	configuration, configured := d.configurations[window]
	return configuration, configured
}

func (d *device) CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error) {
	return d.backend.CreateCommandEncoder(descriptor)
}

func (d *device) Submit(buffers ...CommandBuffer) error { return d.backend.Submit(buffers...) }

func (d *device) MapBuffer(buffer Buffer, mode MapMode, offset, size uint64) error {
	panic("gfx listing: no bodies")
}

func (d *device) MappedRange(buffer Buffer, offset, size uint64) ([]byte, error) {
	panic("gfx listing: no bodies")
}

func (d *device) UnmapBuffer(buffer Buffer) error { panic("gfx listing: no bodies") }

func (d *device) Poll() { panic("gfx listing: no bodies") }

func (d *device) RequestCapture(window platform.Window) error { panic("gfx listing: no bodies") }

// createAutoLayout creates a bind-group layout of each of groups, as the
// backend reflected them for a pipeline's "auto" layout, and a pipeline
// layout of them. When a creation fails, it releases what it created and
// answers the error.
func (d *device) createAutoLayout(groups []BindGroupLayoutDescriptor) (PipelineLayout, error) {
	groupLayouts := make([]BindGroupLayout, 0, len(groups))
	for _, group := range groups {
		groupLayout, err := d.CreateBindGroupLayout(group)
		if err != nil {
			for _, created := range groupLayouts {
				d.ReleaseBindGroupLayout(created)
			}
			return PipelineLayout{}, err
		}
		groupLayouts = append(groupLayouts, groupLayout)
	}
	layout, err := d.CreatePipelineLayout(PipelineLayoutDescriptor{BindGroupLayouts: groupLayouts})
	if err != nil {
		for _, created := range groupLayouts {
			d.ReleaseBindGroupLayout(created)
		}
		return PipelineLayout{}, err
	}
	return layout, nil
}

// releaseAutoLayout releases layout, which createAutoLayout created, and the
// bind-group layouts it holds.
func (d *device) releaseAutoLayout(layout PipelineLayout) {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	d.ReleasePipelineLayout(layout)
	for _, groupLayout := range descriptor.BindGroupLayouts {
		d.ReleaseBindGroupLayout(groupLayout)
	}
}

// groupDescriptors answers the descriptors of layout's bind-group layouts,
// none for the zero layout.
func (d *device) groupDescriptors(layout PipelineLayout) []BindGroupLayoutDescriptor {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	groups := make([]BindGroupLayoutDescriptor, len(descriptor.BindGroupLayouts))
	for index, groupLayout := range descriptor.BindGroupLayouts {
		groups[index], _ = d.bindGroupLayouts.descriptor(groupLayout)
	}
	return groups
}

// groupLayout answers the layout of group in layout, or the zero layout when
// layout is stale or group past its last.
func (d *device) groupLayout(layout PipelineLayout, group uint32) BindGroupLayout {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	if group >= uint32(len(descriptor.BindGroupLayouts)) {
		return BindGroupLayout{}
	}
	return descriptor.BindGroupLayouts[group]
}

// exclusiveDevice is the device as ElevateDeviceAccess answers it, with the
// operations only the write lock may reach.
type exclusiveDevice struct {
	*device
}

func (d exclusiveDevice) WaitIdle() error { return d.backend.WaitIdle() }

// RequestSurface replaces the surface request and marks every surface for
// configuring, so the next frame's configure reaches the backend before its
// acquire; a window opened after is configured from it.
func (d exclusiveDevice) RequestSurface(request SurfaceRequest) {
	d.surfaceRequest = request
	for _, window := range d.surfaces {
		d.frames.markUnconfigured(window)
	}
}

// textureViewSource is what a texture view was created from: its texture and
// its descriptor.
type textureViewSource struct {
	texture    Texture
	descriptor TextureViewDescriptor
}

// pipelineSource is what a pipeline was created with: its descriptor D, its
// layout always explicit, and whether gfx created that layout from the
// backend's reflection, an "auto" one released with the pipeline.
type pipelineSource[D any] struct {
	descriptor D
	autoLayout bool
}

// createResource acquires a handle in resources holding descriptor and has
// the backend create its object with createObject. When that fails, the
// handle is handed back, and the zero handle answered with the error.
func createResource[H ~struct{ handle }, D any](resources *table[H, D], descriptor D, createObject func(H, D) error) (H, error) {
	h := resources.acquire(descriptor)
	if err := createObject(h, descriptor); err != nil {
		_ = resources.retire(h)
		resources.release(h)
		var zero H
		return zero, err
	}
	return h, nil
}

// releaseResource makes h stale in resources, has the backend release its
// object with releaseObject, and hands its index out again; a stale h logs
// ErrReleased through logger and does nothing.
func releaseResource[H ~struct{ handle }, D any](logger *kernel.Logger, resources *table[H, D], h H, releaseObject func(H)) {
	if err := resources.retire(h); err != nil {
		logger.LogErr(err)
		return
	}
	releaseObject(h)
	resources.release(h)
}
