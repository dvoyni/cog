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
	// textureViews holds each texture view at its gfx index.
	textureViews container.ObjectTable[atomic.Pointer[wgpu.TextureView]]
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

func (*device) CreateTexture(gfx.Texture, gfx.TextureDescriptor) error {
	return unbuilt("CreateTexture")
}
func (*device) ReleaseTexture(gfx.Texture) {}
func (*device) CreateTextureView(gfx.TextureView, gfx.Texture, gfx.TextureViewDescriptor) error {
	return unbuilt("CreateTextureView")
}
func (*device) ReleaseTextureView(gfx.TextureView) {}
func (*device) CreateSampler(gfx.Sampler, gfx.SamplerDescriptor) error {
	return unbuilt("CreateSampler")
}
func (*device) ReleaseSampler(gfx.Sampler) {}
func (*device) CreateQuerySet(gfx.QuerySet, gfx.QuerySetDescriptor) error {
	return unbuilt("CreateQuerySet")
}
func (*device) ReleaseQuerySet(gfx.QuerySet) {}
func (*device) TimestampPeriod() float32     { return 0 }
func (*device) CreateShaderModule(gfx.ShaderModule, gfx.ShaderModuleDescriptor) error {
	return unbuilt("CreateShaderModule")
}
func (*device) ReleaseShaderModule(gfx.ShaderModule) {}
func (*device) CreateBindGroupLayout(gfx.BindGroupLayout, gfx.BindGroupLayoutDescriptor) error {
	return unbuilt("CreateBindGroupLayout")
}
func (*device) ReleaseBindGroupLayout(gfx.BindGroupLayout) {}
func (*device) CreateBindGroup(gfx.BindGroup, gfx.BindGroupDescriptor) error {
	return unbuilt("CreateBindGroup")
}
func (*device) ReleaseBindGroup(gfx.BindGroup) {}
func (*device) CreatePipelineLayout(gfx.PipelineLayout, gfx.PipelineLayoutDescriptor) error {
	return unbuilt("CreatePipelineLayout")
}
func (*device) ReleasePipelineLayout(gfx.PipelineLayout) {}
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

func (*device) WriteTexture(gfx.ImageCopyTexture, []byte, gfx.ImageDataLayout, gfx.Extent3D) error {
	return unbuilt("WriteTexture")
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
