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
	// buffers holds each buffer, and its latest map, at its gfx index.
	buffers container.ObjectTable[bufferSlot]
	// textures holds each texture at its gfx index. A framebuffer's index
	// holds none: gogpu offers no surface texture.
	textures container.ObjectTable[atomic.Pointer[wgpu.Texture]]
	// textureViews holds each texture view at its gfx index, a framebuffer's
	// from its acquire to its present.
	textureViews container.ObjectTable[atomic.Pointer[wgpu.TextureView]]
	// samplers holds each sampler at its gfx index.
	samplers container.ObjectTable[atomic.Pointer[wgpu.Sampler]]
	// shaderModules holds each shader module at its gfx index.
	shaderModules container.ObjectTable[atomic.Pointer[wgpu.ShaderModule]]
	// bindGroupLayouts holds each bind group layout at its gfx index.
	bindGroupLayouts container.ObjectTable[atomic.Pointer[wgpu.BindGroupLayout]]
	// bindGroups holds each bind group at its gfx index.
	bindGroups container.ObjectTable[atomic.Pointer[wgpu.BindGroup]]
	// pipelineLayouts holds each pipeline layout at its gfx index.
	pipelineLayouts container.ObjectTable[atomic.Pointer[wgpu.PipelineLayout]]
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
// module's index.
func (d *device) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	created, err := d.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: descriptor.Label, WGSL: descriptor.WGSL})
	if err != nil {
		return err
	}
	d.shaderModules.Grow(module.Index()).Store(created)
	return nil
}

// ReleaseShaderModule drops the module stored at module's index and releases
// it; wgpu destroys it once the submits made before finish.
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
func (*device) ReflectRenderPipelineLayout(gfx.RenderPipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	return nil, unbuilt("ReflectRenderPipelineLayout")
}
func (*device) ReflectComputePipelineLayout(gfx.ComputePipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	return nil, unbuilt("ReflectComputePipelineLayout")
}
func (*device) ReflectVertexInputs(gfx.ShaderModule, string) ([]gfx.VertexInput, error) {
	return nil, unbuilt("ReflectVertexInputs")
}
func (*device) CreateRenderPipeline(gfx.RenderPipeline, gfx.RenderPipelineDescriptor) error {
	return unbuilt("CreateRenderPipeline")
}
func (*device) ReleaseRenderPipeline(gfx.RenderPipeline) {}
func (*device) CreateComputePipeline(gfx.ComputePipeline, gfx.ComputePipelineDescriptor) error {
	return unbuilt("CreateComputePipeline")
}
func (*device) ReleaseComputePipeline(gfx.ComputePipeline) {}

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
	dataLayout := wgpu.ImageDataLayout(layout)
	extent := wgpu.Extent3D(size)
	return d.device.Queue().WriteTexture(&wgpu.ImageCopyTexture{
		Texture:  written,
		MipLevel: destination.MipLevel,
		Origin:   wgpu.Origin3D(destination.Origin),
		Aspect:   gputypes.TextureAspect(destination.Aspect),
	}, data, &dataLayout, &extent)
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
// share the queue, so the indices rise from one with gaps.
func (d *device) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	commandBuffers := make([]*wgpu.CommandBuffer, len(buffers))
	for index, buffer := range buffers {
		commandBuffers[index] = buffer.(*wgpu.CommandBuffer)
	}
	return d.device.Queue().Submit(commandBuffers...)
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
// itself, at the window's size, when its frame begins.
func (d *device) ConfigureSurface(window platform.Window, configuration gfx.SurfaceConfiguration) error {
	capabilities := d.SurfaceCapabilities(window)
	if !slices.Contains(capabilities.Formats, configuration.Format) || !slices.Contains(capabilities.PresentModes, configuration.PresentMode) {
		return fmt.Errorf("gogpu: a surface is %v with %v, not %v with %v", capabilities.Formats[0], capabilities.PresentModes[0], configuration.Format, configuration.PresentMode)
	}
	return nil
}

// AcquireFramebuffer begins window's frame through the HostLoop, on the main
// thread, and stores its surface view at view's index. gogpu offers no
// surface texture, so texture's index stays empty. A window hidden,
// minimized, closed by gogpu or whose compositor holds its frame is
// AcquireStatusTimeout, as are gogpu's timeout; its outdated and lost surface
// are AcquireStatusOutdated and AcquireStatusLost. A captured frame is not
// built yet.
func (d *device) AcquireFramebuffer(window platform.Window, _ gfx.Texture, view gfx.TextureView, captured bool) (gfx.AcquireStatus, error) {
	if captured {
		return 0, unbuilt("a captured AcquireFramebuffer")
	}
	context, ok, err := d.loop.BeginFrame(d.windows[window])
	switch {
	case errors.Is(err, wgpu.ErrSurfaceOutdated):
		return gfx.AcquireStatusOutdated, nil
	case errors.Is(err, wgpu.ErrSurfaceLost):
		return gfx.AcquireStatusLost, nil
	case errors.Is(err, wgpu.ErrTimeout):
		return gfx.AcquireStatusTimeout, nil
	case err != nil:
		return 0, err
	case !ok:
		return gfx.AcquireStatusTimeout, nil
	}
	d.frames[window] = acquiredFrame{context: context, view: view}
	d.textureViews.Grow(view.Index()).Store(context.SurfaceView())
	return gfx.AcquireStatusAcquired, nil
}

// Present ends window's frame through the HostLoop, on the main thread,
// which submits gogpu's own work and presents, and answers its error. The
// surface view stored at the acquire is dropped.
func (d *device) Present(window platform.Window) error {
	frame, ok := d.frames[window]
	if !ok {
		return fmt.Errorf("gogpu: presenting %v, whose frame was not acquired", window)
	}
	delete(d.frames, window)
	d.textureViews.At(frame.view.Index()).Store(nil)
	return d.loop.EndFrame(frame.context)
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

// unbuilt answers the error of a backend method not built yet, naming it.
func unbuilt(method string) error { return fmt.Errorf("gogpu: %s is not built yet", method) }

// acquiredFrame is a window's frame begun and not yet ended.
type acquiredFrame struct {
	context *host.Context
	// view is the gfx view its surface view is stored at.
	view gfx.TextureView
}

// bufferSlot is the buffer at a gfx index and the latest map begun on it,
// each read and written atomically, as gfx's jobs reach a slot from any
// goroutine. A map is never handed back to wgpu's pool, where another map
// could take it while gfx still asks its status: it goes with the garbage.
type bufferSlot struct {
	buffer     atomic.Pointer[wgpu.Buffer]
	pendingMap atomic.Pointer[wgpu.MapPending]
}
