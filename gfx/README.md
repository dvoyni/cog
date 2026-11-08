# gfx

`github.com/dvoyni/cog/gfx` is the GPU as a Cog kernel sees it: a super-thin
mirror of wgpu, in its own types, that draws one frame per beat of the render
track. It is its own Go module (Go 1.27) and depends on nothing but
[kernel](../kernel), [metronome](../metronome) and [platform](../platform).
A backend implements it: the gogpu plugin over our wgpu fork, or the
recording adapter in its `recording` package for tests. It knows nothing of
materials, scenes or the ECS.

Status: being built. Every declaration is in this module. The device comes
up in `Start` and is released in `Destroy`, every resource
is created and released, `CreateQuerySet` answering
`ErrQuerySetsUnavailable` until the wgpu fork exposes query sets, a pipeline with a zero layout getting the
layouts the backend reflects created as explicit ones, pipeline
creation checking the vertex buffers against the shader's inputs and the
pipeline against the device's limits, each
window's surface is created when it opens, configured before its first
acquire and again once resized or `RequestSurface` changes the request, skipped while hidden or zero-size, and
released after the last present when it closes, and each render beat
acquires every window's framebuffer, handling each acquire status as
the design says, publishes `DrawEvent`, submits the
frame's encoders in submit order and presents, writes reach the
backend, and buffers map asynchronously, `BufferMappedEvent` published
from the poll that finds a map resolved or failed, the failure's error
logged, and `Destroy` unmaps and releases the buffers of the maps still
pending, and a capture requested on a configured window of an 8-bit
format is taken from the frame acquired after the request, copied out
after every submit order
and delivered by `CapturedEvent`, its buffer released after the
publication, and `ReadCapture` answers its picture,
and a device loss from any backend call is logged and published once, no
call reaching the backend after it, every call with an error answering
`ErrDeviceLost`, `PreprocessShader` aside, and the frame in flight skipping its submit and present,
the maps and captures pending at it publishing failures, and the next
beat recovers instead of drawing, every object recreated under its old
handle, every surface marked and `DeviceRecoveredEvent` published, a
recovery that fails logged once and retried at most once a second, and
`DrawEvent` carries `AcquireWait` and `GPULatency`, read from
metronome's clock;
the recording adapter rejects a handle missing at its index, a
resource without its usage, a misaligned write, copy or clear, a broken
pass sequence and any query set, reports every feature but timestamp
queries, and its buffers hold the bytes written, copied and cleared,
which its own maps read, and answers a pipeline's reflection with the
layouts a test queues, a vertex entry point's with its inputs, and a
shader's preprocessing with the file's text as it is.
What remains is on the [gfx map](https://github.com/dvoyni/cog/issues/608).

## Shape

- **`InstanceSocket`** is the backend seam, wgpu's instance mirrored, taking
  exactly one binding; a kernel with none panics in `Spawn`.
- **`Device`** is the GPU as every job shares it, read with
  `kernel.Read[gfx.Device]`: creating and releasing resources, writing and
  mapping them, recording and submitting. Every operation is one of its
  methods. Under the write lock, **`ElevateDeviceAccess`** gives the
  **`ExclusiveDevice`**, for `WaitIdle` and `RequestSurface`.
- **Handles** (`Buffer`, `Texture`, `TextureView`, `Sampler`,
  `ShaderModule`, `BindGroupLayout`, `BindGroup`, `PipelineLayout`,
  `RenderPipeline`, `ComputePipeline`, `QuerySet`) are pointer-free and
  generational; zero is none, and a stale one answers `ErrReleased`. They
  survive a device loss.
- **`RenderEvent`** is the beat of the render track the application declares;
  gfx's handler on it draws the frame. **`DrawEvent`** is published inside
  it once the frame's framebuffers are acquired, carrying the **`Frame`** to
  record into, `Ready`, and two timing signals, `AcquireWait` and
  `GPULatency`.
- **`Device.Encoder(frame, order)`** answers a frame's encoder at a
  **`SubmitOrder`**; gfx finishes them and submits them sorted by order.
  **`Device.Framebuffer(frame, window)`** answers a window's framebuffer
  view. **`CreateCommandEncoder`** answers a standalone encoder its owner
  finishes and submits.
- **`SurfaceRequest`** is what every window's surface is configured with:
  `BGRA8Unorm` and Fifo unless asked otherwise.
- **`MapBuffer`**, **`BufferMappedEvent`**, **`RequestCapture`**,
  **`CapturedEvent`** and **`ReadCapture`** are readback and capture,
  asynchronous, delivered by events.
- **`DeviceLostEvent`** and **`DeviceRecoveredEvent`** frame a device loss:
  gfx recreates every object in place, and owners refill their contents.
- **`PreprocessShader`** has the backend handle a WGSL file's directives,
  defined with `ShaderDefine`s; a shader it refuses answers an
  `ErrShaderSource`. The gogpu plugin's preprocessor is `gogpu/shader`.

## Kernel surface

- **Resources it installs:**
  - `Device`: the GPU, read under its read lock, elevated under its write
    lock.
  - `Tasks`: the surface tasks and the device recovery, for a backend's
    plugin to pin to the main thread where its platform needs that; gfx pins
    none and schedules them itself. `ReleaseOpenSurfaces` among them is what
    `Stop` schedules, to release the surfaces of the windows still open
    before any `Destroy`; `RecoverDevice` is what `Draw` schedules in place
    of a frame after a loss, pinned by a backend whose device replacement
    touches main-thread state.
  - `Handlers`: `Draw`, `CreateSurface`, `ResizeSurface` and
    `ReleaseSurface`, to order handlers around.
- **Resources it reads from other plugins:** `*platform.WindowManager`, for
  windows' native handles, framebuffer sizes and whether they are hidden;
  `metronome.Clock`, in its `Init`, to make the stopwatches `DrawEvent`'s
  timing signals are measured by.
- **Events it publishes:** `DrawEvent`, inside its `RenderEvent` handler;
  a private stop event, from `Stop`, whose handler schedules
  `Tasks.ReleaseOpenSurfaces`;
  `BufferMappedEvent` and `CapturedEvent`, from the job that finds them
  resolved; `DeviceLostEvent` and `DeviceRecoveredEvent`.
- **Events it subscribes to:** `RenderEvent` (`Handlers.Draw`), which draws
  the frame; `platform.WindowOpenedEvent` (`CreateSurface`),
  `platform.ResizeEvent` (`ResizeSurface`) and `platform.WindowClosedEvent`
  (`ReleaseSurface`), which keep a surface per window.
- **Sockets it declares:** `InstanceSocket`, required and exclusive, for the
  backend.
- **Sockets it binds:** none.

gfx requires platform and metronome, listed before it, and checks them
with `Dependencies`: it makes its stopwatches from metronome's `Clock`. A plugin that reads `Handlers` or
`Tasks` in its `Init` is listed after gfx: the backend's plugin, which binds
`InstanceSocket` and pins the tasks its platform needs on the main thread,
goes after gfx for that.

## Wiring

```go
supervisor.Spawn(map[kernel.PluginName]any{
	metronome.PluginName: &metronome.Config{Tracks: []metronome.Track{
		metronome.NewTrack[gfx.RenderEvent](vsync), // MaxInFlight 1, MinInterval 1/240 s
	}},
}, workerCount,
	platform.NewPlugin(),
	metronome.NewPlugin(),
	gfx.NewPlugin(),
	gogpu.NewPlugin(), // binds gfx.InstanceSocket and platform's seams, pins gfx.Tasks
	game.NewPlugin(), // subscribes to gfx.DrawEvent
)
```

A game records a frame from a task scheduled after `e.Ready`, with the event
as its arg, and joins it into its out dependency:

```go
func (p *game) Init(c *kernel.Configurator, _ any) error {
	record := c.RegisterTask2(recordScene)
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		return e, ex.Join(in, ex.Schedule(record, e, e.Ready).Handle())
	})
	return nil
}

func recordScene(k *kernel.Kernel, e gfx.DrawEvent, device kernel.Read[gfx.Device], scene kernel.Read[*Scene]) kernel.NoResult {
	encoder, err := device.Get().Encoder(e.Frame, orderScene)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments: []gfx.RenderPassColorAttachment{{
			View:    device.Get().Framebuffer(e.Frame, scene.Get().Window),
			LoadOp:  gfx.LoadOpClear,
			StoreOp: gfx.StoreOpStore,
		}},
	})
	pass.SetPipeline(scene.Get().Pipeline)
	pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
	pass.End()
	return kernel.NoResult{}
}
```

Shader modules and pipelines are slow to create: a game creates them from a
task of its own under `kernel.Read[gfx.Device]`, at load or in the
background, never from a frame's recording.

See [docs/README.md](docs/README.md) for the guide, and
[docs/design.md](docs/design.md) for the design and why it is this way.
