package gfx

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// Handlers is the resource holding every event handler gfx subscribes, so a
// plugin listed after gfx orders its own around them with
// configurator.OrderSubscriptions. gfx offers no tasks: acquire, flush and
// present are its own.
type Handlers struct {
	// Draw handles RenderEvent. After a device loss it schedules the pinned
	// recovery under the write lock instead of a frame, and returns it.
	// Otherwise it schedules a pinned configure of the
	// surfaces marked for it, under the write lock, then the pinned acquire,
	// each after the last present; publishes DrawEvent; and schedules the
	// flush and the pinned present after the nested publication's out
	// dependency, which it returns.
	Draw kernel.Sub[RenderEvent]
	// CreateSurface handles platform.WindowOpenedEvent: it schedules the
	// pinned creation of the window's surface and marks it for configuring.
	CreateSurface kernel.Sub[platform.WindowOpenedEvent]
	// ResizeSurface handles platform.ResizeEvent: it marks the window's
	// surface for configuring at its new framebuffer size, and schedules
	// nothing.
	ResizeSurface kernel.Sub[platform.ResizeEvent]
	// ReleaseSurface handles platform.WindowClosedEvent: it schedules the
	// pinned release of the window's surface after the last present.
	ReleaseSurface kernel.Sub[platform.WindowClosedEvent]
}

// Device is the resource of the GPU as every job shares it, read with
// kernel.Read[gfx.Device]: creating and releasing resources, writing them,
// recording and submitting. Jobs holding the read lock use it at once.
// Methods that answer an error answer ErrReleased for a released handle.
type Device interface {
	// Features answers the features the device was created with.
	Features() Features
	// Limits answers the limits the device was created with.
	Limits() Limits
	// AdapterInfo answers what GPU the device runs on.
	AdapterInfo() AdapterInfo

	CreateBuffer(descriptor BufferDescriptor) (Buffer, error)
	ReleaseBuffer(buffer Buffer)
	// BufferDescriptor answers the descriptor buffer was created with.
	BufferDescriptor(buffer Buffer) BufferDescriptor

	CreateTexture(descriptor TextureDescriptor) (Texture, error)
	ReleaseTexture(texture Texture)
	// TextureDescriptor answers the descriptor texture was created with.
	TextureDescriptor(texture Texture) TextureDescriptor

	CreateTextureView(texture Texture, descriptor TextureViewDescriptor) (TextureView, error)
	ReleaseTextureView(view TextureView)
	// TextureViewTexture answers the texture view is of.
	TextureViewTexture(view TextureView) Texture

	CreateSampler(descriptor SamplerDescriptor) (Sampler, error)
	ReleaseSampler(sampler Sampler)

	// CreateQuerySet creates a query set; it answers an error until the
	// wgpu fork exposes query sets.
	CreateQuerySet(descriptor QuerySetDescriptor) (QuerySet, error)
	ReleaseQuerySet(querySet QuerySet)
	// TimestampPeriod answers the nanoseconds of one timestamp tick.
	TimestampPeriod() float32

	// CreateShaderModule compiles the module and reflects it once, keeping
	// the result with its handle for the "auto" layouts of the pipelines
	// made from it. Like the pipeline creations, it is slow and synchronous:
	// a game calls it from a task of its own, scheduled anywhere, never
	// from a frame's recording.
	CreateShaderModule(descriptor ShaderModuleDescriptor) (ShaderModule, error)
	ReleaseShaderModule(module ShaderModule)

	CreateBindGroupLayout(descriptor BindGroupLayoutDescriptor) (BindGroupLayout, error)
	ReleaseBindGroupLayout(layout BindGroupLayout)

	CreateBindGroup(descriptor BindGroupDescriptor) (BindGroup, error)
	ReleaseBindGroup(group BindGroup)

	CreatePipelineLayout(descriptor PipelineLayoutDescriptor) (PipelineLayout, error)
	ReleasePipelineLayout(layout PipelineLayout)

	// CreateRenderPipeline creates a pipeline, synchronously, from a task
	// off the frame. Each call answers its own handle; sharing pipelines of
	// one descriptor is the caller's. A compile holds the read lock, so a
	// surface configure, and the frame behind it, waits for it; and a burst
	// of compiles can take every worker from a frame's recording, so a game
	// bounds how many run at once. The backend caches pipelines on disk where
	// it can (Vulkan, DX12), outside platform's FS; gfx adds no cache.
	CreateRenderPipeline(descriptor RenderPipelineDescriptor) (RenderPipeline, error)
	ReleaseRenderPipeline(pipeline RenderPipeline)
	// RenderPipelineBindGroupLayout answers the layout of group in
	// pipeline's layout, the one reflection built when it was created
	// "auto".
	RenderPipelineBindGroupLayout(pipeline RenderPipeline, group uint32) BindGroupLayout

	// CreateComputePipeline creates a pipeline as CreateRenderPipeline does.
	CreateComputePipeline(descriptor ComputePipelineDescriptor) (ComputePipeline, error)
	ReleaseComputePipeline(pipeline ComputePipeline)
	// ComputePipelineBindGroupLayout answers the layout of group in
	// pipeline's layout.
	ComputePipelineBindGroupLayout(pipeline ComputePipeline, group uint32) BindGroupLayout

	// WriteBuffer writes data into buffer at offset; offset and length are
	// multiples of 4. It is the upload path: data is copied out before it
	// returns, so the caller's slice is free at once, and lands before every
	// command buffer of the next submit, whichever frame or standalone
	// encoder that submit carries, so two writes to one region in a frame
	// leave the last for the whole frame. Each call takes the backend's queue
	// lock: write a task's whole region once, not once per draw.
	WriteBuffer(buffer Buffer, offset uint64, data []byte) error
	// WriteTexture writes data, laid out as layout, into the size region at
	// destination, as WriteBuffer writes a buffer.
	WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error

	// Encoder answers frame's encoder at order, which gfx's flush finishes
	// and submits sorted by order; an encoder whose Finish fails goes to
	// LogErr and is dropped, and the rest are submitted. Take one per task
	// that records in parallel, not one per draw. An order already handed out
	// in frame answers ErrOrderTaken, and a frame flushed or presented
	// ErrFrameFlushed, each with an encoder that records into nothing.
	Encoder(frame Frame, order SubmitOrder) (CommandEncoder, error)
	// Framebuffer answers the view of window's framebuffer in frame, until
	// frame is presented, or the zero view when frame acquired none for it
	// (minimized, hidden, or not open) or is presented.
	Framebuffer(frame Frame, window platform.Window) TextureView
	// SurfaceConfiguration answers what window's surface is configured
	// with, for pipelines to target its format; false when it has none:
	// hidden, a zero framebuffer, or not yet configured.
	SurfaceConfiguration(window platform.Window) (SurfaceConfiguration, bool)

	// CreateCommandEncoder answers an encoder outside any frame, for its
	// owner to finish and submit.
	CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error)
	// Submit submits command buffers from standalone encoders, in order.
	Submit(buffers ...CommandBuffer) error

	// MapBuffer starts mapping buffer's range for mode, and returns at once;
	// BufferMappedEvent says when it resolved. There is no blocking map: it
	// would wait for the whole device to go idle. A kernel with no frames
	// running calls Poll from the job that maps.
	MapBuffer(buffer Buffer, mode MapMode, offset, size uint64) error
	// MappedRange answers the mapped bytes of buffer's range, valid until
	// UnmapBuffer; offset is a multiple of 8 and size of 4.
	MappedRange(buffer Buffer, offset, size uint64) ([]byte, error)
	// UnmapBuffer ends buffer's map.
	UnmapBuffer(buffer Buffer) error
	// Poll resolves the maps whose submits have finished, without waiting,
	// publishing their events; every submit polls too.
	Poll()

	// RequestCapture asks for window's framebuffer in the next frame: gfx's
	// flush copies it after every submit order and before the present, and
	// CapturedEvent delivers it a frame later. A window already being
	// captured answers ErrCaptureBusy, and one whose surface is not
	// SurfaceRequest.Capturable, or whose format is not 8-bit BGRA or RGBA,
	// ErrCaptureUnsupported.
	RequestCapture(window platform.Window) error
}

// ElevateDeviceAccess answers the Device a job holds the write lock on as an
// ExclusiveDevice. Only the write handle gives it, so a job holding the read
// lock cannot reach the exclusive operations.
func ElevateDeviceAccess(handle kernel.Write[Device]) ExclusiveDevice {
	panic("gfx listing: no bodies")
}

// ExclusiveDevice is the Device as one job alone holds it, for what no other
// job may overlap. ElevateDeviceAccess gives it; it is no resource.
type ExclusiveDevice interface {
	Device
	// WaitIdle returns once the GPU has finished everything submitted.
	WaitIdle() error

	// RequestSurface replaces the surface request and marks every surface to
	// be configured again, between frames, from the next frame on.
	RequestSurface(request SurfaceRequest)
}
