package gogpu

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel/container"
	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/wgpu"
)

// device is gogpu's device as gfx's backend device. A method not built yet
// answers an error naming it, and does nothing where it answers no error.
type device struct {
	device *wgpu.Device
	// loop is the HostLoop the device's frames begin and end through.
	loop *host.HostLoop
	// windows is the plugin's map of the windows open to gogpu's, read on
	// the main thread alone.
	windows map[platform.Window]*host.Window
	// frames holds each window's frame acquired and not yet presented, on
	// the main thread alone.
	frames map[platform.Window]acquiredFrame
	// surfaceSizes holds the size each window's surface is configured with,
	// which a captured frame's offscreen texture takes, set by its configure
	// under gfx's write lock and read by its acquires on the main thread. A
	// closed window's size is left in it.
	surfaceSizes map[platform.Window]wgpu.Extent3D
	// offscreenGroupLayout and offscreenPipeline draw an offscreen texture
	// onto a window, made at the first captured acquire.
	offscreenGroupLayout *wgpu.BindGroupLayout
	offscreenPipeline    *wgpu.RenderPipeline
	// buffers holds each buffer, and its latest map, at its gfx index.
	buffers container.ObjectTable[bufferSlot]
	// textures holds each texture at its gfx index, and a captured frame's
	// offscreen texture at its framebuffer's from its acquire to its present.
	// Another framebuffer's index holds none: gogpu offers no surface
	// texture.
	textures container.ObjectTable[atomic.Pointer[wgpu.Texture]]
	// textureViews holds each texture view at its gfx index, a framebuffer's
	// from its acquire to its present: the surface's, or a captured frame's
	// offscreen view.
	textureViews container.ObjectTable[atomic.Pointer[wgpu.TextureView]]
	// samplers holds each sampler at its gfx index.
	samplers container.ObjectTable[atomic.Pointer[wgpu.Sampler]]
	// shaderModules holds each shader module, with its reflection, at its
	// gfx index.
	shaderModules container.ObjectTable[atomic.Pointer[shaderModule]]
	// bindGroupLayouts holds each bind group layout at its gfx index.
	bindGroupLayouts container.ObjectTable[atomic.Pointer[wgpu.BindGroupLayout]]
	// bindGroups holds each bind group at its gfx index.
	bindGroups container.ObjectTable[atomic.Pointer[wgpu.BindGroup]]
	// pipelineLayouts holds each pipeline layout at its gfx index.
	pipelineLayouts container.ObjectTable[atomic.Pointer[wgpu.PipelineLayout]]
	// renderPipelines holds each render pipeline at its gfx index.
	renderPipelines container.ObjectTable[atomic.Pointer[wgpu.RenderPipeline]]
	// computePipelines holds each compute pipeline at its gfx index.
	computePipelines container.ObjectTable[atomic.Pointer[wgpu.ComputePipeline]]
}

// CreateBuffer creates wgpu's buffer and stores it at buffer's index.
func (d *device) CreateBuffer(buffer gfx.Buffer, descriptor gfx.BufferDescriptor) error {
	created, err := d.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            descriptor.Label,
		Size:             descriptor.Size,
		Usage:            gputypes.BufferUsage(descriptor.Usage),
		MappedAtCreation: descriptor.MappedAtCreation,
	})
	if err != nil {
		return err
	}
	d.buffers.Grow(buffer.Index()).buffer.Store(created)
	return nil
}

// ReleaseBuffer drops the buffer stored at buffer's index and releases it;
// wgpu keeps it until the submits using it finish.
func (d *device) ReleaseBuffer(buffer gfx.Buffer) {
	if slot := d.buffers.At(buffer.Index()); slot != nil {
		if released := slot.buffer.Swap(nil); released != nil {
			released.Release()
		}
	}
}

// CreateTexture creates wgpu's texture and stores it at texture's index.
func (d *device) CreateTexture(texture gfx.Texture, descriptor gfx.TextureDescriptor) error {
	viewFormats := make([]gputypes.TextureFormat, len(descriptor.ViewFormats))
	for index, format := range descriptor.ViewFormats {
		viewFormats[index] = gputypes.TextureFormat(format)
	}
	created, err := d.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         descriptor.Label,
		Size:          wgpu.Extent3D(descriptor.Size),
		MipLevelCount: descriptor.MipLevelCount,
		SampleCount:   descriptor.SampleCount,
		Dimension:     gputypes.TextureDimension(descriptor.Dimension),
		Format:        gputypes.TextureFormat(descriptor.Format),
		Usage:         gputypes.TextureUsage(descriptor.Usage),
		ViewFormats:   viewFormats,
	})
	if err != nil {
		return err
	}
	d.textures.Grow(texture.Index()).Store(created)
	return nil
}

// ReleaseTexture drops the texture stored at texture's index and releases
// it; wgpu keeps it while a view of it or a submit using it lives.
func (d *device) ReleaseTexture(texture gfx.Texture) { releaseStored(&d.textures, texture.Index()) }

// CreateTextureView creates wgpu's view of the texture stored at texture's
// index, and stores it at view's index.
func (d *device) CreateTextureView(view gfx.TextureView, texture gfx.Texture, descriptor gfx.TextureViewDescriptor) error {
	viewed, err := storedObject(&d.textures, texture, "texture")
	if err != nil {
		return err
	}
	created, err := d.device.CreateTextureView(viewed, &wgpu.TextureViewDescriptor{
		Label:           descriptor.Label,
		Format:          gputypes.TextureFormat(descriptor.Format),
		Dimension:       gputypes.TextureViewDimension(descriptor.Dimension),
		Aspect:          gputypes.TextureAspect(descriptor.Aspect),
		BaseMipLevel:    descriptor.BaseMipLevel,
		MipLevelCount:   descriptor.MipLevelCount,
		BaseArrayLayer:  descriptor.BaseArrayLayer,
		ArrayLayerCount: descriptor.ArrayLayerCount,
	})
	if err != nil {
		return err
	}
	d.textureViews.Grow(view.Index()).Store(created)
	return nil
}

// ReleaseTextureView drops the view stored at view's index and releases it;
// wgpu keeps it while a submit using it lives.
func (d *device) ReleaseTextureView(view gfx.TextureView) {
	releaseStored(&d.textureViews, view.Index())
}

// CreateSampler creates wgpu's sampler and stores it at sampler's index. A
// zero LodMaxClamp clamps nothing, as wgpu takes it.
func (d *device) CreateSampler(sampler gfx.Sampler, descriptor gfx.SamplerDescriptor) error {
	created, err := d.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        descriptor.Label,
		AddressModeU: gputypes.AddressMode(descriptor.AddressModeU),
		AddressModeV: gputypes.AddressMode(descriptor.AddressModeV),
		AddressModeW: gputypes.AddressMode(descriptor.AddressModeW),
		MagFilter:    gputypes.FilterMode(descriptor.MagFilter),
		MinFilter:    gputypes.FilterMode(descriptor.MinFilter),
		MipmapFilter: gputypes.FilterMode(descriptor.MipmapFilter),
		LodMinClamp:  descriptor.LodMinClamp,
		LodMaxClamp:  descriptor.LodMaxClamp,
		Compare:      gputypes.CompareFunction(descriptor.Compare),
		Anisotropy:   descriptor.Anisotropy,
	})
	if err != nil {
		return err
	}
	d.samplers.Grow(sampler.Index()).Store(created)
	return nil
}

// ReleaseSampler drops the sampler stored at sampler's index and releases it;
// wgpu keeps it while a bind group or a submit using it lives.
func (d *device) ReleaseSampler(sampler gfx.Sampler) { releaseStored(&d.samplers, sampler.Index()) }

func (*device) CreateQuerySet(gfx.QuerySet, gfx.QuerySetDescriptor) error {
	return unbuilt("CreateQuerySet")
}
func (*device) ReleaseQuerySet(gfx.QuerySet) {}
func (*device) TimestampPeriod() float32     { return 0 }

// CreateShaderModule creates wgpu's module, whose WGSL naga parses and
// validates, answering its error naming the problem, and stores it at
// module's index, with its reflection, which naga parses again for.
func (d *device) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	created, err := d.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: descriptor.Label, WGSL: descriptor.WGSL})
	if err != nil {
		return err
	}
	reflection, err := reflectModule(descriptor.WGSL)
	if err != nil {
		created.Release()
		return err
	}
	d.shaderModules.Grow(module.Index()).Store(&shaderModule{ShaderModule: created, reflection: reflection})
	return nil
}

// ReleaseShaderModule drops the module stored at module's index, with its
// reflection, and releases it; wgpu destroys it once the submits made before
// finish.
func (d *device) ReleaseShaderModule(module gfx.ShaderModule) {
	releaseStored(&d.shaderModules, module.Index())
}

// CreateBindGroupLayout creates wgpu's layout and stores it at layout's
// index. An entry's bindings of a zero type are none.
func (d *device) CreateBindGroupLayout(layout gfx.BindGroupLayout, descriptor gfx.BindGroupLayoutDescriptor) error {
	entries := make([]gputypes.BindGroupLayoutEntry, len(descriptor.Entries))
	for index, entry := range descriptor.Entries {
		entries[index] = gputypes.BindGroupLayoutEntry{Binding: entry.Binding, Visibility: gputypes.ShaderStages(entry.Visibility)}
		if entry.Buffer.Type != gfx.BufferBindingTypeUndefined {
			entries[index].Buffer = &gputypes.BufferBindingLayout{
				Type:             gputypes.BufferBindingType(entry.Buffer.Type),
				HasDynamicOffset: entry.Buffer.HasDynamicOffset,
				MinBindingSize:   entry.Buffer.MinBindingSize,
			}
		}
		if entry.Sampler.Type != gfx.SamplerBindingTypeUndefined {
			entries[index].Sampler = &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingType(entry.Sampler.Type)}
		}
		if entry.Texture.SampleType != gfx.TextureSampleTypeUndefined {
			entries[index].Texture = &gputypes.TextureBindingLayout{
				SampleType:    gputypes.TextureSampleType(entry.Texture.SampleType),
				ViewDimension: gputypes.TextureViewDimension(entry.Texture.ViewDimension),
				Multisampled:  entry.Texture.Multisampled,
			}
		}
		if entry.StorageTexture.Access != gfx.StorageTextureAccessUndefined {
			entries[index].StorageTexture = &gputypes.StorageTextureBindingLayout{
				Access:        gputypes.StorageTextureAccess(entry.StorageTexture.Access),
				Format:        gputypes.TextureFormat(entry.StorageTexture.Format),
				ViewDimension: gputypes.TextureViewDimension(entry.StorageTexture.ViewDimension),
			}
		}
	}
	created, err := d.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: descriptor.Label, Entries: entries})
	if err != nil {
		return err
	}
	d.bindGroupLayouts.Grow(layout.Index()).Store(created)
	return nil
}

// ReleaseBindGroupLayout drops the layout stored at layout's index and
// releases it; wgpu destroys it once the submits made before finish.
func (d *device) ReleaseBindGroupLayout(layout gfx.BindGroupLayout) {
	releaseStored(&d.bindGroupLayouts, layout.Index())
}

// CreateBindGroup creates wgpu's bind group, over the layout and of the
// objects stored at the descriptor's indexes, and stores it at group's index.
// An entry's zero handles bind none.
func (d *device) CreateBindGroup(group gfx.BindGroup, descriptor gfx.BindGroupDescriptor) error {
	layout, err := storedObject(&d.bindGroupLayouts, descriptor.Layout, "bind group layout")
	if err != nil {
		return err
	}
	entries := make([]wgpu.BindGroupEntry, len(descriptor.Entries))
	for index, entry := range descriptor.Entries {
		entries[index] = wgpu.BindGroupEntry{Binding: entry.Binding, Offset: entry.Offset, Size: entry.Size}
		if entry.Buffer != (gfx.Buffer{}) {
			if entries[index].Buffer, err = d.storedBuffer(entry.Buffer); err != nil {
				return err
			}
		}
		if entry.Sampler != (gfx.Sampler{}) {
			if entries[index].Sampler, err = storedObject(&d.samplers, entry.Sampler, "sampler"); err != nil {
				return err
			}
		}
		if entry.TextureView != (gfx.TextureView{}) {
			if entries[index].TextureView, err = storedObject(&d.textureViews, entry.TextureView, "texture view"); err != nil {
				return err
			}
		}
	}
	created, err := d.device.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: descriptor.Label, Layout: layout, Entries: entries})
	if err != nil {
		return err
	}
	d.bindGroups.Grow(group.Index()).Store(created)
	return nil
}

// ReleaseBindGroup drops the bind group stored at group's index and releases
// it; wgpu keeps it while a submit using it lives.
func (d *device) ReleaseBindGroup(group gfx.BindGroup) { releaseStored(&d.bindGroups, group.Index()) }

// CreatePipelineLayout creates wgpu's layout of the bind group layouts stored
// at the descriptor's indexes, and stores it at layout's index.
func (d *device) CreatePipelineLayout(layout gfx.PipelineLayout, descriptor gfx.PipelineLayoutDescriptor) error {
	groupLayouts := make([]*wgpu.BindGroupLayout, len(descriptor.BindGroupLayouts))
	for group, groupLayout := range descriptor.BindGroupLayouts {
		stored, err := storedObject(&d.bindGroupLayouts, groupLayout, "bind group layout")
		if err != nil {
			return err
		}
		groupLayouts[group] = stored
	}
	created, err := d.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{Label: descriptor.Label, BindGroupLayouts: groupLayouts})
	if err != nil {
		return err
	}
	d.pipelineLayouts.Grow(layout.Index()).Store(created)
	return nil
}

// ReleasePipelineLayout drops the layout stored at layout's index and
// releases it; wgpu destroys it once the submits made before finish.
func (d *device) ReleasePipelineLayout(layout gfx.PipelineLayout) {
	releaseStored(&d.pipelineLayouts, layout.Index())
}

// ReflectRenderPipelineLayout answers the layouts WebGPU's "auto" builds of
// the bindings the vertex entry point and the fragment one, when there is a
// fragment module, use, from their modules' reflections.
func (d *device) ReflectRenderPipelineLayout(descriptor gfx.RenderPipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	vertex, err := d.reflectedEntryPoint(descriptor.Vertex.Module, ir.StageVertex, descriptor.Vertex.EntryPoint)
	if err != nil {
		return nil, err
	}
	if descriptor.Fragment.Module == (gfx.ShaderModule{}) {
		return reflectedGroupLayouts(vertex), nil
	}
	fragment, err := d.reflectedEntryPoint(descriptor.Fragment.Module, ir.StageFragment, descriptor.Fragment.EntryPoint)
	if err != nil {
		return nil, err
	}
	return reflectedGroupLayouts(vertex, fragment), nil
}

// ReflectComputePipelineLayout answers the layouts WebGPU's "auto" builds of
// the bindings the compute entry point uses, from its module's reflection.
func (d *device) ReflectComputePipelineLayout(descriptor gfx.ComputePipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	compute, err := d.reflectedEntryPoint(descriptor.Module, ir.StageCompute, descriptor.EntryPoint)
	if err != nil {
		return nil, err
	}
	return reflectedGroupLayouts(compute), nil
}

// ReflectVertexInputs answers no inputs until the plugin reflects its
// modules, so gfx's check at pipeline creation passes every vertex buffer,
// and wgpu checks none against the shader either.
func (*device) ReflectVertexInputs(gfx.ShaderModule, string) ([]gfx.VertexInput, error) {
	return nil, nil
}

// CreateRenderPipeline creates wgpu's pipeline over the layout and modules
// stored at the descriptor's indexes, and stores it at pipeline's index. A
// zero DepthStencil format, fragment module, strip index format or blend is
// none; wgpu takes the WebGPU defaults for zero stencil faces, blend
// components and sample mask. An empty entry point is its module's one of
// its stage, named to wgpu.
func (d *device) CreateRenderPipeline(pipeline gfx.RenderPipeline, descriptor gfx.RenderPipelineDescriptor) error {
	layout, err := storedObject(&d.pipelineLayouts, descriptor.Layout, "pipeline layout")
	if err != nil {
		return err
	}
	vertexModule, err := storedObject(&d.shaderModules, descriptor.Vertex.Module, "shader module")
	if err != nil {
		return err
	}
	vertexEntryPoint, err := vertexModule.reflection.entryPoint(ir.StageVertex, descriptor.Vertex.EntryPoint)
	if err != nil {
		return err
	}
	buffers := make([]gputypes.VertexBufferLayout, len(descriptor.Vertex.Buffers))
	for index, buffer := range descriptor.Vertex.Buffers {
		buffers[index] = gputypes.VertexBufferLayout{ArrayStride: buffer.ArrayStride, StepMode: gputypes.VertexStepMode(buffer.StepMode)}
		for _, attribute := range buffer.Attributes {
			buffers[index].Attributes = append(buffers[index].Attributes, gputypes.VertexAttribute{
				Format: gputypes.VertexFormat(attribute.Format), Offset: attribute.Offset, ShaderLocation: attribute.ShaderLocation,
			})
		}
	}
	primitive := descriptor.Primitive
	created := &wgpu.RenderPipelineDescriptor{
		Label:  descriptor.Label,
		Layout: layout,
		Vertex: wgpu.VertexState{Module: vertexModule.ShaderModule, EntryPoint: vertexEntryPoint.name, Buffers: buffers},
		Primitive: gputypes.PrimitiveState{
			Topology:       gputypes.PrimitiveTopology(primitive.Topology),
			FrontFace:      gputypes.FrontFace(primitive.FrontFace),
			CullMode:       gputypes.CullMode(primitive.CullMode),
			UnclippedDepth: primitive.UnclippedDepth,
		},
		Multisample: gputypes.MultisampleState(descriptor.Multisample),
	}
	if primitive.StripIndexFormat != gfx.IndexFormatUndefined {
		stripIndexFormat := gputypes.IndexFormat(primitive.StripIndexFormat)
		created.Primitive.StripIndexFormat = &stripIndexFormat
	}
	if depthStencil := descriptor.DepthStencil; depthStencil.Format != gfx.TextureFormatUndefined {
		created.DepthStencil = &wgpu.DepthStencilState{
			Format:              gputypes.TextureFormat(depthStencil.Format),
			DepthWriteEnabled:   depthStencil.DepthWriteEnabled,
			DepthCompare:        gputypes.CompareFunction(depthStencil.DepthCompare),
			StencilFront:        stencilFaceState(depthStencil.StencilFront),
			StencilBack:         stencilFaceState(depthStencil.StencilBack),
			StencilReadMask:     depthStencil.StencilReadMask,
			StencilWriteMask:    depthStencil.StencilWriteMask,
			DepthBias:           depthStencil.DepthBias,
			DepthBiasSlopeScale: depthStencil.DepthBiasSlopeScale,
			DepthBiasClamp:      depthStencil.DepthBiasClamp,
		}
	}
	if fragment := descriptor.Fragment; fragment.Module != (gfx.ShaderModule{}) {
		fragmentModule, err := storedObject(&d.shaderModules, fragment.Module, "shader module")
		if err != nil {
			return err
		}
		fragmentEntryPoint, err := fragmentModule.reflection.entryPoint(ir.StageFragment, fragment.EntryPoint)
		if err != nil {
			return err
		}
		targets := make([]gputypes.ColorTargetState, len(fragment.Targets))
		for index, target := range fragment.Targets {
			targets[index] = gputypes.ColorTargetState{Format: gputypes.TextureFormat(target.Format), WriteMask: gputypes.ColorWriteMask(target.WriteMask)}
			if target.Blend != (gfx.BlendState{}) {
				targets[index].Blend = &gputypes.BlendState{Color: blendComponent(target.Blend.Color), Alpha: blendComponent(target.Blend.Alpha)}
			}
		}
		created.Fragment = &wgpu.FragmentState{Module: fragmentModule.ShaderModule, EntryPoint: fragmentEntryPoint.name, Targets: targets}
	}
	renderPipeline, err := d.device.CreateRenderPipeline(created)
	if err != nil {
		return err
	}
	d.renderPipelines.Grow(pipeline.Index()).Store(renderPipeline)
	return nil
}

// ReleaseRenderPipeline drops the pipeline stored at pipeline's index and
// releases it; wgpu keeps it while a submit using it lives.
func (d *device) ReleaseRenderPipeline(pipeline gfx.RenderPipeline) {
	releaseStored(&d.renderPipelines, pipeline.Index())
}

// CreateComputePipeline creates wgpu's pipeline over the layout and module
// stored at the descriptor's indexes, and stores it at pipeline's index. An
// empty entry point is the module's one compute entry point, named to wgpu.
func (d *device) CreateComputePipeline(pipeline gfx.ComputePipeline, descriptor gfx.ComputePipelineDescriptor) error {
	layout, err := storedObject(&d.pipelineLayouts, descriptor.Layout, "pipeline layout")
	if err != nil {
		return err
	}
	module, err := storedObject(&d.shaderModules, descriptor.Module, "shader module")
	if err != nil {
		return err
	}
	entryPoint, err := module.reflection.entryPoint(ir.StageCompute, descriptor.EntryPoint)
	if err != nil {
		return err
	}
	created, err := d.device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:      descriptor.Label,
		Layout:     layout,
		Module:     module.ShaderModule,
		EntryPoint: entryPoint.name,
		Constants:  descriptor.Constants,
	})
	if err != nil {
		return err
	}
	d.computePipelines.Grow(pipeline.Index()).Store(created)
	return nil
}

// ReleaseComputePipeline drops the pipeline stored at pipeline's index and
// releases it; wgpu keeps it while a submit using it lives.
func (d *device) ReleaseComputePipeline(pipeline gfx.ComputePipeline) {
	releaseStored(&d.computePipelines, pipeline.Index())
}

// WriteBuffer writes data through wgpu's queue, which copies it out and
// lands it at its next submit.
func (d *device) WriteBuffer(buffer gfx.Buffer, offset uint64, data []byte) error {
	written, err := d.storedBuffer(buffer)
	if err != nil {
		return err
	}
	return d.device.Queue().WriteBuffer(written, offset, data)
}

// WriteTexture writes data through wgpu's queue into the texture stored at
// destination's index, landing at its next submit as a buffer's write does.
func (d *device) WriteTexture(destination gfx.ImageCopyTexture, data []byte, layout gfx.ImageDataLayout, size gfx.Extent3D) error {
	written, err := storedObject(&d.textures, destination.Texture, "texture")
	if err != nil {
		return err
	}
	location := makeImageCopyTexture(written, destination)
	dataLayout := wgpu.ImageDataLayout(layout)
	extent := wgpu.Extent3D(size)
	return d.device.Queue().WriteTexture(&location, data, &dataLayout, &extent)
}

// MapBuffer begins wgpu's map, which resolves once the submits made before it
// finish, and keeps it at buffer's index for MapStatus.
func (d *device) MapBuffer(buffer gfx.Buffer, mode gfx.MapMode, offset, size uint64) error {
	mapped, err := d.storedBuffer(buffer)
	if err != nil {
		return err
	}
	pendingMap, err := mapped.MapAsync(wgpu.MapMode(mode), offset, size)
	if err != nil {
		return err
	}
	d.buffers.At(buffer.Index()).pendingMap.Store(pendingMap)
	return nil
}

// MapStatus answers the status of the latest map begun on buffer, as wgpu's
// Poll left it.
func (d *device) MapStatus(buffer gfx.Buffer) (ready bool, err error) {
	slot := d.buffers.At(buffer.Index())
	if slot == nil {
		return false, fmt.Errorf("gogpu: %v holds no buffer", buffer)
	}
	return slot.pendingMap.Load().Status()
}

// MappedRange answers the bytes of wgpu's mapped range. wgpu forbids ranges
// that overlap one already answered in the same map.
func (d *device) MappedRange(buffer gfx.Buffer, offset, size uint64) ([]byte, error) {
	mapped, err := d.storedBuffer(buffer)
	if err != nil {
		return nil, err
	}
	mappedRange, err := mapped.MappedRange(offset, size)
	if err != nil {
		return nil, err
	}
	defer mappedRange.Release()
	return mappedRange.Bytes(), nil
}

// UnmapBuffer ends wgpu's map, a pending one canceled.
func (d *device) UnmapBuffer(buffer gfx.Buffer) error {
	mapped, err := d.storedBuffer(buffer)
	if err != nil {
		return err
	}
	return mapped.Unmap()
}

// Poll resolves the maps whose submits wgpu has seen finish, and answers the
// newest submission its queue has finished.
func (d *device) Poll() uint64 {
	d.device.Poll(wgpu.PollPoll)
	return d.device.Queue().Poll()
}

func (d *device) CreateCommandEncoder(descriptor gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	encoder, err := d.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: descriptor.Label})
	if err != nil {
		return nil, err
	}
	return &commandEncoder{device: d, encoder: encoder}, nil
}

// Submit submits buffers, the command buffers the encoders' Finish answered,
// on wgpu's queue, and answers its submission index. gogpu's own submits
// share the queue, so the indices rise from one with gaps. A device lost is
// gfx's ErrDeviceLost.
func (d *device) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	commandBuffers := make([]*wgpu.CommandBuffer, len(buffers))
	for index, buffer := range buffers {
		commandBuffers[index] = buffer.(*wgpu.CommandBuffer)
	}
	submission, err := d.device.Queue().Submit(commandBuffers...)
	return submission, markDeviceLoss(err)
}

// SurfaceCapabilities answers what gogpu configures every surface with:
// BGRA8Unorm, Fifo and Opaque. gogpu's vsync picks FifoRelaxed where Vulkan
// offers it, as Fifo that tears when a frame is late.
func (*device) SurfaceCapabilities(platform.Window) gfx.SurfaceCapabilities {
	return gfx.SurfaceCapabilities{
		Formats:      []gfx.TextureFormat{gfx.TextureFormatBGRA8Unorm},
		PresentModes: []gfx.PresentMode{gfx.PresentModeFifo},
		AlphaModes:   []gfx.CompositeAlphaMode{gfx.CompositeAlphaModeOpaque},
	}
}

// ConfigureSurface accepts what SurfaceCapabilities answers, and answers an
// error for any other format or present mode: gogpu configures the surface
// itself, at the window's size, when its frame begins. A captured frame's
// offscreen texture takes configuration's size.
func (d *device) ConfigureSurface(window platform.Window, configuration gfx.SurfaceConfiguration) error {
	capabilities := d.SurfaceCapabilities(window)
	if !slices.Contains(capabilities.Formats, configuration.Format) || !slices.Contains(capabilities.PresentModes, configuration.PresentMode) {
		return fmt.Errorf("gogpu: a surface is %v with %v, not %v with %v", capabilities.Formats[0], capabilities.PresentModes[0], configuration.Format, configuration.PresentMode)
	}
	d.surfaceSizes[window] = wgpu.Extent3D{Width: configuration.Width, Height: configuration.Height, DepthOrArrayLayers: 1}
	return nil
}

// AcquireFramebuffer begins window's frame through the HostLoop, on the main
// thread, and stores its surface view at view's index. gogpu offers no
// surface texture, so texture's index stays empty. A captured frame stores
// an offscreen texture made for it and its view instead, and keeps the
// surface view for the present; when that texture fails to be made, the
// frame ends with nothing presented, since gogpu begins the surface's frame
// only when its view is asked for, and the error is answered. A window
// hidden, minimized, closed by gogpu or whose compositor holds its frame is
// AcquireStatusTimeout, as are gogpu's timeout; its outdated and lost surface
// are AcquireStatusOutdated and AcquireStatusLost, and a device lost is
// gfx's ErrDeviceLost.
func (d *device) AcquireFramebuffer(window platform.Window, texture gfx.Texture, view gfx.TextureView, captured bool) (gfx.AcquireStatus, error) {
	context, ok, err := d.loop.BeginFrame(d.windows[window])
	switch {
	case errors.Is(err, wgpu.ErrSurfaceOutdated):
		return gfx.AcquireStatusOutdated, nil
	case errors.Is(err, wgpu.ErrSurfaceLost):
		return gfx.AcquireStatusLost, nil
	case errors.Is(err, wgpu.ErrTimeout):
		return gfx.AcquireStatusTimeout, nil
	case err != nil:
		return 0, markDeviceLoss(err)
	case !ok:
		return gfx.AcquireStatusTimeout, nil
	}
	var offscreen *offscreenTexture
	if captured {
		if offscreen, err = d.newOffscreenTexture(d.surfaceSizes[window]); err != nil {
			return 0, markDeviceLoss(errors.Join(err, d.loop.EndFrame(context)))
		}
	}
	surfaceView := context.SurfaceView()
	d.frames[window] = acquiredFrame{context: context, surfaceView: surfaceView, texture: texture, view: view, offscreen: offscreen}
	stored := surfaceView
	if offscreen != nil {
		d.textures.Grow(texture.Index()).Store(offscreen.texture)
		stored = offscreen.view
	}
	d.textureViews.Grow(view.Index()).Store(stored)
	return gfx.AcquireStatusAcquired, nil
}

// Present ends window's frame through the HostLoop, on the main thread,
// which submits gogpu's own work and presents, and answers its error, a
// device lost as gfx's ErrDeviceLost. A captured frame's offscreen texture is
// drawn onto the surface first, then released, and the frame still ends when
// that draw fails. The views and texture stored at the acquire are dropped.
func (d *device) Present(window platform.Window) error {
	frame, ok := d.frames[window]
	if !ok {
		return fmt.Errorf("gogpu: presenting %v, whose frame was not acquired", window)
	}
	delete(d.frames, window)
	d.textureViews.At(frame.view.Index()).Store(nil)
	if frame.offscreen == nil {
		return markDeviceLoss(d.loop.EndFrame(frame.context))
	}
	d.textures.At(frame.texture.Index()).Store(nil)
	err := d.drawOffscreenTexture(frame.offscreen, frame.surfaceView)
	frame.offscreen.destroy()
	return markDeviceLoss(errors.Join(err, d.loop.EndFrame(frame.context)))
}

func (*device) WaitIdle() error { return unbuilt("WaitIdle") }

// Release does nothing: gogpu's device goes with the HostLoop.
func (*device) Release() {}

// storedBuffer answers the buffer stored at buffer's index, or an error when
// it holds none.
func (d *device) storedBuffer(buffer gfx.Buffer) (*wgpu.Buffer, error) {
	var stored *wgpu.Buffer
	if slot := d.buffers.At(buffer.Index()); slot != nil {
		stored = slot.buffer.Load()
	}
	if stored == nil {
		return nil, fmt.Errorf("gogpu: %v holds no buffer", buffer)
	}
	return stored, nil
}

// reflectedEntryPoint answers the reflection of module's entry point of stage
// named name, or of its one entry point of stage when name is empty.
func (d *device) reflectedEntryPoint(module gfx.ShaderModule, stage ir.ShaderStage, name string) (*entryPointReflection, error) {
	stored, err := storedObject(&d.shaderModules, module, "shader module")
	if err != nil {
		return nil, err
	}
	return stored.reflection.entryPoint(stage, name)
}

// storedObject answers the object stored in objects at handle's index, or an
// error naming the object when it holds none, as a framebuffer's texture
// index does.
func storedObject[T any](objects *container.ObjectTable[atomic.Pointer[T]], handle interface{ Index() uint32 }, object string) (*T, error) {
	var stored *T
	if slot := objects.At(handle.Index()); slot != nil {
		stored = slot.Load()
	}
	if stored == nil {
		return nil, fmt.Errorf("gogpu: %v holds no %s", handle, object)
	}
	return stored, nil
}

// releaseStored drops the object stored at index in objects, and releases it.
func releaseStored[T any, P interface {
	*T
	Release()
}](objects *container.ObjectTable[atomic.Pointer[T]], index uint32) {
	if slot := objects.At(index); slot != nil {
		if released := P(slot.Swap(nil)); released != nil {
			released.Release()
		}
	}
}

// stencilFaceState answers gfx's stencil face as wgpu's.
func stencilFaceState(face gfx.StencilFaceState) wgpu.StencilFaceState {
	return wgpu.StencilFaceState{
		Compare:     gputypes.CompareFunction(face.Compare),
		FailOp:      gputypes.StencilOperation(face.FailOp),
		DepthFailOp: gputypes.StencilOperation(face.DepthFailOp),
		PassOp:      gputypes.StencilOperation(face.PassOp),
	}
}

// blendComponent answers gfx's blend component as gputypes'.
func blendComponent(component gfx.BlendComponent) gputypes.BlendComponent {
	return gputypes.BlendComponent{
		SrcFactor: gputypes.BlendFactor(component.SrcFactor),
		DstFactor: gputypes.BlendFactor(component.DstFactor),
		Operation: gputypes.BlendOperation(component.Operation),
	}
}

// unbuilt answers the error of a backend method not built yet, naming it.
func unbuilt(method string) error { return fmt.Errorf("gogpu: %s is not built yet", method) }

// markDeviceLoss answers err as gfx's ErrDeviceLost, wrapping it, when it is
// a device loss: wgpu's typed one, which Vulkan answers, or DX12's removed
// device. Any other err is answered as it is.
func markDeviceLoss(err error) error {
	if err != nil && (errors.Is(err, wgpu.ErrDeviceLost) || isDeviceRemoved(err)) {
		return fmt.Errorf("%w: %w", gfx.ErrDeviceLost, err)
	}
	return err
}

// shaderModule is wgpu's shader module and its reflection, stored together at
// the module's gfx index.
type shaderModule struct {
	*wgpu.ShaderModule
	reflection moduleReflection
}

// acquiredFrame is a window's frame begun and not yet ended.
type acquiredFrame struct {
	context     *host.Context
	surfaceView *wgpu.TextureView
	// texture and view are the gfx texture and view its framebuffer is
	// stored at.
	texture gfx.Texture
	view    gfx.TextureView
	// offscreen is the offscreen texture made for the frame when it is
	// captured, and nil otherwise.
	offscreen *offscreenTexture
}

// bufferSlot is the buffer at a gfx index and the latest map begun on it,
// each read and written atomically, as gfx's jobs reach a slot from any
// goroutine. A map is never handed back to wgpu's pool, where another map
// could take it while gfx still asks its status: it goes with the garbage.
type bufferSlot struct {
	buffer     atomic.Pointer[wgpu.Buffer]
	pendingMap atomic.Pointer[wgpu.MapPending]
}
