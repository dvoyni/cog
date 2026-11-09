package gogpu

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
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
	// mutex guards textureViews.
	mutex sync.Mutex
	// textureViews holds each texture view at its gfx index.
	textureViews []*wgpu.TextureView
}

func (*device) CreateBuffer(gfx.Buffer, gfx.BufferDescriptor) error { return unbuilt("CreateBuffer") }
func (*device) ReleaseBuffer(gfx.Buffer)                            {}
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
func (*device) ReleaseComputePipeline(gfx.ComputePipeline)   {}
func (*device) WriteBuffer(gfx.Buffer, uint64, []byte) error { return unbuilt("WriteBuffer") }
func (*device) WriteTexture(gfx.ImageCopyTexture, []byte, gfx.ImageDataLayout, gfx.Extent3D) error {
	return unbuilt("WriteTexture")
}
func (*device) MapBuffer(gfx.Buffer, gfx.MapMode, uint64, uint64) error { return unbuilt("MapBuffer") }
func (*device) MapStatus(gfx.Buffer) (bool, error)                      { return false, unbuilt("MapStatus") }
func (*device) MappedRange(gfx.Buffer, uint64, uint64) ([]byte, error) {
	return nil, unbuilt("MappedRange")
}
func (*device) UnmapBuffer(gfx.Buffer) error { return unbuilt("UnmapBuffer") }
func (*device) Poll() uint64                 { return 0 }
func (*device) CreateCommandEncoder(gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	return nil, unbuilt("CreateCommandEncoder")
}
func (*device) Submit(...gfx.CommandBuffer) (uint64, error) { return 0, unbuilt("Submit") }

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
	d.storeTextureView(view, context.SurfaceView())
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
	d.storeTextureView(frame.view, nil)
	return d.loop.EndFrame(frame.context)
}

func (*device) WaitIdle() error { return unbuilt("WaitIdle") }

// Release does nothing: gogpu's device goes with the HostLoop.
func (*device) Release() {}

// storeTextureView stores textureView at view's index.
func (d *device) storeTextureView(view gfx.TextureView, textureView *wgpu.TextureView) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if index := int(view.Index()); index >= len(d.textureViews) {
		d.textureViews = slices.Grow(d.textureViews, index+1-len(d.textureViews))[:index+1]
	}
	d.textureViews[view.Index()] = textureView
}

// unbuilt answers the error of a backend method not built yet, naming it.
func unbuilt(method string) error { return fmt.Errorf("gogpu: %s is not built yet", method) }

// acquiredFrame is a window's frame begun and not yet ended.
type acquiredFrame struct {
	context *host.Context
	// view is the gfx view its surface view is stored at.
	view gfx.TextureView
}
