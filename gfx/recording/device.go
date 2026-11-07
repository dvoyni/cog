package recording

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// device is the recording device and its queue, as adapter.RequestDevice
// answers it. A create, write or map naming a handle missing at its index, or
// a resource without the usage the call puts it to, answers an error from the
// call, as wgpu does; the objects a create names are those of descriptors and
// handles not zero, a zero handle naming none. Every map resolves at once.
type device struct {
	instance *Instance
	// Each table holds its type's objects, as what each was created with,
	// at their indices, under the instance's mutex.
	buffers          objectTable[*bufferObject]
	textures         objectTable[gfx.TextureDescriptor]
	textureViews     objectTable[textureView]
	samplers         objectTable[gfx.SamplerDescriptor]
	shaderModules    objectTable[gfx.ShaderModuleDescriptor]
	bindGroupLayouts objectTable[gfx.BindGroupLayoutDescriptor]
	bindGroups       objectTable[gfx.BindGroupDescriptor]
	pipelineLayouts  objectTable[gfx.PipelineLayoutDescriptor]
	renderPipelines  objectTable[gfx.RenderPipelineDescriptor]
	computePipelines objectTable[gfx.ComputePipelineDescriptor]
	// pendingWrites are the buffer writes made since the last submit, which
	// the next applies before its command buffers.
	pendingWrites []func()
	// submissionCount is how many submits this device made, the last one's
	// index, under the instance's mutex.
	submissionCount uint64
}

// CreateBuffer records the creation and keeps buffer at its index, its bytes
// zeroed.
func (d *device) CreateBuffer(buffer gfx.Buffer, descriptor gfx.BufferDescriptor) error {
	return d.instance.record(CallKindCreateBuffer, CreateBufferArguments{Buffer: buffer, Descriptor: descriptor}, func() error {
		return d.buffers.store(buffer.Index(), &bufferObject{descriptor: descriptor, data: make([]byte, descriptor.Size)})
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

// CreateTextureView records the creation and keeps view at its index, with
// its texture's usage; an error when texture is missing.
func (d *device) CreateTextureView(view gfx.TextureView, texture gfx.Texture, descriptor gfx.TextureViewDescriptor) error {
	arguments := CreateTextureViewArguments{View: view, Texture: texture, Descriptor: descriptor}
	return d.instance.record(CallKindCreateTextureView, arguments, func() error {
		textureDescriptor, err := requireObject(&d.textures, texture, "the viewed texture")
		if err != nil {
			return err
		}
		return d.textureViews.store(view.Index(), textureView{arguments: arguments, usage: textureDescriptor.Usage})
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

// CreateQuerySet records the creation and answers gfx.ErrQuerySetsUnavailable:
// query sets wait on the wgpu fork, so the device keeps none.
func (d *device) CreateQuerySet(querySet gfx.QuerySet, descriptor gfx.QuerySetDescriptor) error {
	return d.instance.record(CallKindCreateQuerySet, CreateQuerySetArguments{QuerySet: querySet, Descriptor: descriptor}, func() error {
		return gfx.ErrQuerySetsUnavailable
	}).Error
}

// ReleaseQuerySet records the release, with an error, since no query set is
// kept.
func (d *device) ReleaseQuerySet(querySet gfx.QuerySet) {
	d.instance.record(CallKindReleaseQuerySet, querySet, func() error { return requireQuerySet(querySet, "the released query set") })
}

// TimestampPeriod answers TimestampPeriod.
func (*device) TimestampPeriod() float32 { return TimestampPeriod }

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

// CreateBindGroup records the creation and keeps group at its index; an
// error when its layout or a resource is missing, or a resource lacks the
// usage its layout entry binds it for: a uniform buffer Uniform, a storage
// buffer Storage, a storage texture StorageBinding.
func (d *device) CreateBindGroup(group gfx.BindGroup, descriptor gfx.BindGroupDescriptor) error {
	return d.instance.record(CallKindCreateBindGroup, CreateBindGroupArguments{Group: group, Descriptor: descriptor}, func() error {
		layout, err := requireObject(&d.bindGroupLayouts, descriptor.Layout, "the bind group's layout")
		if err != nil {
			return err
		}
		for _, entry := range descriptor.Entries {
			var layoutEntry gfx.BindGroupLayoutEntry
			if index := slices.IndexFunc(layout.Entries, func(candidate gfx.BindGroupLayoutEntry) bool { return candidate.Binding == entry.Binding }); index >= 0 {
				layoutEntry = layout.Entries[index]
			}
			bufferUsage := gfx.BufferUsageFlagNone
			switch layoutEntry.Buffer.Type {
			case gfx.BufferBindingTypeUniform:
				bufferUsage = gfx.BufferUsageFlagUniform
			case gfx.BufferBindingTypeStorage, gfx.BufferBindingTypeReadOnlyStorage:
				bufferUsage = gfx.BufferUsageFlagStorage
			}
			textureUsage := gfx.TextureUsageFlagNone
			if layoutEntry.StorageTexture.Access != gfx.StorageTextureAccessUndefined {
				textureUsage = gfx.TextureUsageFlagStorageBinding
			}
			role := fmt.Sprintf("binding %d", entry.Binding)
			if err := d.requireBuffer(entry.Buffer, bufferUsage, role); err != nil {
				return err
			}
			if _, err := requireObject(&d.samplers, entry.Sampler, role); err != nil {
				return err
			}
			if err := d.requireView(entry.TextureView, textureUsage, role); err != nil {
				return err
			}
		}
		return d.bindGroups.store(group.Index(), descriptor)
	}).Error
}

// ReleaseBindGroup records the release and drops group from its index.
func (d *device) ReleaseBindGroup(group gfx.BindGroup) {
	d.instance.record(CallKindReleaseBindGroup, group, func() error { return d.bindGroups.remove(group.Index()) })
}

// CreatePipelineLayout records the creation and keeps layout at its index;
// an error when a group layout is missing.
func (d *device) CreatePipelineLayout(layout gfx.PipelineLayout, descriptor gfx.PipelineLayoutDescriptor) error {
	return d.instance.record(CallKindCreatePipelineLayout, CreatePipelineLayoutArguments{Layout: layout, Descriptor: descriptor}, func() error {
		for index, groupLayout := range descriptor.BindGroupLayouts {
			if _, err := requireObject(&d.bindGroupLayouts, groupLayout, fmt.Sprintf("group %d's layout", index)); err != nil {
				return err
			}
		}
		return d.pipelineLayouts.store(layout.Index(), descriptor)
	}).Error
}

// ReleasePipelineLayout records the release and drops layout from its index.
func (d *device) ReleasePipelineLayout(layout gfx.PipelineLayout) {
	d.instance.record(CallKindReleasePipelineLayout, layout, func() error { return d.pipelineLayouts.remove(layout.Index()) })
}

// CreateRenderPipeline records the creation and keeps pipeline at its index;
// an error when its layout or a stage's module is missing.
func (d *device) CreateRenderPipeline(pipeline gfx.RenderPipeline, descriptor gfx.RenderPipelineDescriptor) error {
	return d.instance.record(CallKindCreateRenderPipeline, CreateRenderPipelineArguments{Pipeline: pipeline, Descriptor: descriptor}, func() error {
		if _, err := requireObject(&d.pipelineLayouts, descriptor.Layout, "the pipeline's layout"); err != nil {
			return err
		}
		if _, err := requireObject(&d.shaderModules, descriptor.Vertex.Module, "the vertex module"); err != nil {
			return err
		}
		if _, err := requireObject(&d.shaderModules, descriptor.Fragment.Module, "the fragment module"); err != nil {
			return err
		}
		return d.renderPipelines.store(pipeline.Index(), descriptor)
	}).Error
}

// ReleaseRenderPipeline records the release and drops pipeline from its index.
func (d *device) ReleaseRenderPipeline(pipeline gfx.RenderPipeline) {
	d.instance.record(CallKindReleaseRenderPipeline, pipeline, func() error { return d.renderPipelines.remove(pipeline.Index()) })
}

// ReflectRenderPipelineLayout records the reflection and answers the
// BindGroupLayouts AnswerNext queued, copied, or none: the adapter parses no
// WGSL, so a test queues the layouts a real backend would reflect. An error
// when a module is missing.
func (d *device) ReflectRenderPipelineLayout(descriptor gfx.RenderPipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	answer := d.instance.record(CallKindReflectRenderPipelineLayout, descriptor, func() error {
		if _, err := requireObject(&d.shaderModules, descriptor.Vertex.Module, "the vertex module"); err != nil {
			return err
		}
		_, err := requireObject(&d.shaderModules, descriptor.Fragment.Module, "the fragment module")
		return err
	})
	return cloneLayouts(answer.BindGroupLayouts), answer.Error
}

// ReflectComputePipelineLayout records the reflection and answers as
// ReflectRenderPipelineLayout does; an error when its module is missing.
func (d *device) ReflectComputePipelineLayout(descriptor gfx.ComputePipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	answer := d.instance.record(CallKindReflectComputePipelineLayout, descriptor, func() error {
		_, err := requireObject(&d.shaderModules, descriptor.Module, "the compute module")
		return err
	})
	return cloneLayouts(answer.BindGroupLayouts), answer.Error
}

// ReflectVertexInputs records the reflection and answers the VertexInputs
// AnswerNext queued, copied, or none, as ReflectRenderPipelineLayout does;
// an error when module is missing.
func (d *device) ReflectVertexInputs(module gfx.ShaderModule, entryPoint string) ([]gfx.VertexInput, error) {
	answer := d.instance.record(CallKindReflectVertexInputs, ReflectVertexInputsArguments{Module: module, EntryPoint: entryPoint}, func() error {
		_, err := requireObject(&d.shaderModules, module, "the vertex module")
		return err
	})
	return slices.Clone(answer.VertexInputs), answer.Error
}

// CreateComputePipeline records the creation and keeps pipeline at its
// index; an error when its layout or module is missing.
func (d *device) CreateComputePipeline(pipeline gfx.ComputePipeline, descriptor gfx.ComputePipelineDescriptor) error {
	return d.instance.record(CallKindCreateComputePipeline, CreateComputePipelineArguments{Pipeline: pipeline, Descriptor: descriptor}, func() error {
		if _, err := requireObject(&d.pipelineLayouts, descriptor.Layout, "the pipeline's layout"); err != nil {
			return err
		}
		if _, err := requireObject(&d.shaderModules, descriptor.Module, "the compute module"); err != nil {
			return err
		}
		return d.computePipelines.store(pipeline.Index(), descriptor)
	}).Error
}

// ReleaseComputePipeline records the release and drops pipeline from its index.
func (d *device) ReleaseComputePipeline(pipeline gfx.ComputePipeline) {
	d.instance.record(CallKindReleaseComputePipeline, pipeline, func() error { return d.computePipelines.remove(pipeline.Index()) })
}

// WriteBuffer records the write, data copied, which lands at the next
// submit, before its command buffers; an error when buffer is missing or
// lacks CopyDst, offset or data's length is not a multiple of
// copyBufferAlignment, or the write ends past the buffer.
func (d *device) WriteBuffer(buffer gfx.Buffer, offset uint64, data []byte) error {
	data = slices.Clone(data)
	return d.instance.record(CallKindWriteBuffer, WriteBufferArguments{Buffer: buffer, Offset: offset, Data: data}, func() error {
		if err := cmp.Or(
			d.requireBuffer(buffer, gfx.BufferUsageFlagCopyDst, "the written buffer"),
			requireAligned(offset, copyBufferAlignment, "the write's offset"),
			requireAligned(uint64(len(data)), copyBufferAlignment, "the write's length"),
		); err != nil {
			return err
		}
		written, err := d.requireRange(buffer, offset, uint64(len(data)), "the write")
		if err != nil {
			return err
		}
		d.pendingWrites = append(d.pendingWrites, func() { copy(written, data) })
		return nil
	}).Error
}

// WriteTexture records the write, data copied; an error when the texture is
// missing or lacks CopyDst. Textures hold no texels, so it lands nowhere.
func (d *device) WriteTexture(destination gfx.ImageCopyTexture, data []byte, layout gfx.ImageDataLayout, size gfx.Extent3D) error {
	arguments := WriteTextureArguments{Destination: destination, Data: slices.Clone(data), Layout: layout, Size: size}
	return d.instance.record(CallKindWriteTexture, arguments, func() error {
		return d.requireTexture(destination.Texture, gfx.TextureUsageFlagCopyDst, "the written texture")
	}).Error
}

// MapBuffer records the map, which resolves at once; an error when buffer is
// missing, lacks MapRead or MapWrite for mode, is mapped already, or the
// range ends past it.
func (d *device) MapBuffer(buffer gfx.Buffer, mode gfx.MapMode, offset, size uint64) error {
	return d.instance.record(CallKindMapBuffer, MapBufferArguments{Buffer: buffer, Mode: mode, Offset: offset, Size: size}, func() error {
		object, err := d.requireMapState(buffer, false)
		if err != nil {
			return err
		}
		usage := gfx.BufferUsageFlagMapRead
		if mode == gfx.MapModeWrite {
			usage = gfx.BufferUsageFlagMapWrite
		}
		if err := cmp.Or(
			d.requireBuffer(buffer, usage, "the mapped buffer"),
			requireWithin(offset, size, uint64(len(object.data)), "the map"),
		); err != nil {
			return err
		}
		object.mapped = true
		object.mapOffset = offset
		object.mapSize = size
		return nil
	}).Error
}

// MapStatus answers ready for a mapped buffer, every map resolving at once;
// an error when buffer is missing or not mapped, or the error AnswerNext
// queued for CallKindMapStatus, which ends the map.
func (d *device) MapStatus(buffer gfx.Buffer) (ready bool, err error) {
	d.instance.mutex.Lock()
	defer d.instance.mutex.Unlock()
	object, err := d.requireMapState(buffer, true)
	if err != nil {
		return false, err
	}
	if err := d.instance.takeAnswer(CallKindMapStatus).Error; err != nil {
		object.mapped = false
		return false, err
	}
	return true, nil
}

// MappedRange answers the buffer's bytes in the range, which writes into it
// change; an error when buffer is missing or not mapped, or the range is not
// within the map.
func (d *device) MappedRange(buffer gfx.Buffer, offset, size uint64) ([]byte, error) {
	d.instance.mutex.Lock()
	defer d.instance.mutex.Unlock()
	object, err := d.requireMapState(buffer, true)
	if err != nil {
		return nil, err
	}
	if offset < object.mapOffset {
		return nil, fmt.Errorf("recording: the mapped range at %d begins before the map at %d", offset, object.mapOffset)
	}
	if err := requireWithin(offset-object.mapOffset, size, object.mapSize, "the mapped range"); err != nil {
		return nil, err
	}
	return object.data[offset : offset+size : offset+size], nil
}

// UnmapBuffer records the unmap and ends buffer's map; an error when buffer is
// missing or not mapped.
func (d *device) UnmapBuffer(buffer gfx.Buffer) error {
	return d.instance.record(CallKindUnmapBuffer, buffer, func() error {
		object, err := d.requireMapState(buffer, true)
		if err != nil {
			return err
		}
		object.mapped = false
		return nil
	}).Error
}

// Poll answers every submission finished: every map resolves, and every
// submit finishes, at once.
func (d *device) Poll() uint64 {
	d.instance.mutex.Lock()
	defer d.instance.mutex.Unlock()
	return d.submissionCount
}

// CreateCommandEncoder answers an encoder recording the commands given it.
func (d *device) CreateCommandEncoder(descriptor gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	return &commandEncoder{device: d}, nil
}

// Submit records the call and buffers as one submission, after the calls
// recorded before it, and applies the writes pending, then each buffer's
// copies and clears, in order; or it answers the error AnswerNext queued for
// it and submits nothing, answering index zero. Every buffer is one the
// adapter's encoders finished. Its index counts from one on each device.
func (d *device) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	submission := Submission{Buffers: make([]CommandBuffer, len(buffers))}
	for index, buffer := range buffers {
		submission.Buffers[index] = CommandBuffer{Commands: buffer.(*commandBuffer).commands}
	}
	var index uint64
	err := d.instance.record(CallKindSubmit, nil, func() error {
		for _, write := range d.pendingWrites {
			write()
		}
		d.pendingWrites = nil
		for _, buffer := range buffers {
			for _, effect := range buffer.(*commandBuffer).bufferEffects {
				effect()
			}
		}
		d.instance.recording.submittedAfter = append(d.instance.recording.submittedAfter, len(d.instance.recording.Calls))
		d.instance.recording.Submissions = append(d.instance.recording.Submissions, submission)
		d.submissionCount++
		index = d.submissionCount
		return nil
	}).Error
	return index, err
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
		Capturable:   capabilities.Capturable,
	}
}

// ConfigureSurface records the configure and marks window's surface
// configured, capturable as configuration says; an error when window has no surface, or holds a framebuffer
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
		windowSurface.capturable = configuration.Capturable
		d.instance.surfaces[window] = windowSurface
		return nil
	}).Error
}

// AcquireFramebuffer records the acquire and answers the status AnswerNext
// queued, Acquired when none is; a status with a framebuffer keeps texture
// and view as window's until its present. An error when window has no surface, or holds
// a framebuffer not yet presented, as wgpu answers ErrSurfaceAlreadyAcquired,
// or its surface is not configured.
func (d *device) AcquireFramebuffer(window platform.Window, texture gfx.Texture, view gfx.TextureView) (status gfx.AcquireStatus, err error) {
	answer := d.instance.recordAnswered(CallKindAcquireFramebuffer, AcquireFramebufferArguments{Window: window, Texture: texture, View: view}, func(answer Answer) error {
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
			windowSurface.framebufferTexture = texture
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
		windowSurface.framebufferTexture = gfx.Texture{}
		windowSurface.framebuffer = gfx.TextureView{}
		d.instance.surfaces[window] = windowSurface
		return nil
	}).Error
}

// WaitIdle records the wait, and answers what AnswerNext queued for it.
func (d *device) WaitIdle() error { return d.instance.record(CallKindWaitIdle, nil, nil).Error }

// Release records the device released, with every object it holds, and
// leaves each surface unconfigured, dropping a framebuffer acquired and not
// presented, as a real device's release does.
func (d *device) Release() {
	d.instance.record(CallKindReleaseDevice, nil, func() error {
		for window, windowSurface := range d.instance.surfaces {
			d.instance.surfaces[window] = surface{native: windowSurface.native}
		}
		return nil
	})
}

// requireBuffer answers an error when buffer is missing, or lacks usage, for
// role; nil for the zero buffer.
func (d *device) requireBuffer(buffer gfx.Buffer, usage gfx.BufferUsageFlags, role string) error {
	object, err := requireObject(&d.buffers, buffer, role)
	if err != nil || buffer == (gfx.Buffer{}) {
		return err
	}
	if object.descriptor.Usage&usage != usage {
		return fmt.Errorf("recording: %s: the buffer at index %d lacks usage %#x", role, buffer.Index(), usage&^object.descriptor.Usage)
	}
	return nil
}

// requireRange answers the bytes of buffer from offset, size long, for role;
// an error when buffer is missing or the range ends past it, nil bytes for
// the zero buffer.
func (d *device) requireRange(buffer gfx.Buffer, offset, size uint64, role string) ([]byte, error) {
	object, err := requireObject(&d.buffers, buffer, role)
	if err != nil || buffer == (gfx.Buffer{}) {
		return nil, err
	}
	if err := requireWithin(offset, size, uint64(len(object.data)), role); err != nil {
		return nil, err
	}
	return object.data[offset : offset+size : offset+size], nil
}

// requireMapState answers buffer's object when whether it is mapped is
// mapped; an error when buffer is zero or missing, or its map state is the
// other.
func (d *device) requireMapState(buffer gfx.Buffer, mapped bool) (*bufferObject, error) {
	object, err := requireObject(&d.buffers, buffer, "the mapped buffer")
	switch {
	case err != nil:
		return nil, err
	case buffer == (gfx.Buffer{}):
		return nil, errors.New("recording: a map of no buffer")
	case mapped && !object.mapped:
		return nil, fmt.Errorf("recording: the buffer at index %d is not mapped", buffer.Index())
	case !mapped && object.mapped:
		return nil, fmt.Errorf("recording: the buffer at index %d is mapped already", buffer.Index())
	}
	return object, nil
}

// requireTexture answers an error when texture is missing, or lacks usage,
// for role; nil for the zero texture. A framebuffer acquired and not yet
// presented is a texture whose usage is RenderAttachment, and CopySrc when
// its surface is configured capturable.
func (d *device) requireTexture(texture gfx.Texture, usage gfx.TextureUsageFlags, role string) error {
	if texture == (gfx.Texture{}) {
		return nil
	}
	textureUsage, isFramebuffer := d.instance.framebufferUsage(texture)
	if !isFramebuffer {
		descriptor, err := requireObject(&d.textures, texture, role)
		if err != nil {
			return err
		}
		textureUsage = descriptor.Usage
	}
	if textureUsage&usage != usage {
		return fmt.Errorf("recording: %s: the texture at index %d lacks usage %#x", role, texture.Index(), usage&^textureUsage)
	}
	return nil
}

// requireView answers an error when view is missing, or its texture lacks
// usage, for role; nil for the zero view. A framebuffer acquired and not yet
// presented is a view whose usage is RenderAttachment.
func (d *device) requireView(view gfx.TextureView, usage gfx.TextureUsageFlags, role string) error {
	if view == (gfx.TextureView{}) {
		return nil
	}
	viewUsage := gfx.TextureUsageFlagNone
	if d.instance.holdsFramebuffer(view) {
		viewUsage = gfx.TextureUsageFlagRenderAttachment
	} else {
		object, err := requireObject(&d.textureViews, view, role)
		if err != nil {
			return err
		}
		viewUsage = object.usage
	}
	if viewUsage&usage != usage {
		return fmt.Errorf("recording: %s: the view at index %d lacks usage %#x", role, view.Index(), usage&^viewUsage)
	}
	return nil
}

// cloneLayouts answers a copy of layouts, their entries copied, so a test
// changing what it queued changes no answer; nil stays nil.
func cloneLayouts(layouts []gfx.BindGroupLayoutDescriptor) []gfx.BindGroupLayoutDescriptor {
	if layouts == nil {
		return nil
	}
	clones := make([]gfx.BindGroupLayoutDescriptor, len(layouts))
	for index, layout := range layouts {
		layout.Entries = slices.Clone(layout.Entries)
		clones[index] = layout
	}
	return clones
}

// bufferObject is a buffer as the device keeps it: what it was created with,
// its bytes, and its map, if any.
type bufferObject struct {
	descriptor gfx.BufferDescriptor
	data       []byte
	mapped     bool
	// mapOffset and mapSize are the range mapped.
	mapOffset, mapSize uint64
}

// textureView is a view as the device keeps it: what it was created with, and
// its texture's usage, which outlives the texture's release, as a wgpu view
// keeps its texture.
type textureView struct {
	arguments CreateTextureViewArguments
	usage     gfx.TextureUsageFlags
}
