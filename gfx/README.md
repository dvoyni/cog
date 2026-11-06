# gfx

`github.com/dvoyni/cog/gfx` is the GPU as a Cog kernel sees it: a super-thin
mirror of wgpu, in its own types, that draws one frame per beat of the render
track. It is its own Go module (Go 1.27) and depends on nothing but
[kernel](../kernel), [metronome](../metronome) and [platform](../platform).
A backend implements it: the gogpu plugin over our wgpu fork, or the
recording adapter in its `recording` package for tests. It knows nothing of
materials, scenes or the ECS.

Status: being built. Every declaration is in this module. The device comes
up in `Start` and is released in `Destroy`, every resource but query sets
is created and released, pipelines with explicit layouts only, each
window's surface is created when it opens and released after the last
present when it closes, and each render beat acquires every window's
framebuffer, publishes `DrawEvent` and presents; every other body panics until its ticket on the [gfx map](https://github.com/dvoyni/cog/issues/608) fills it.

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
- **`PreprocessShader`** is v1's WGSL preprocessor.

## Kernel surface

- **Resources it installs:**
  - `Device`: the GPU, read under its read lock, elevated under its write
    lock.
  - `Handlers`: `Draw`, `CreateSurface`, `ResizeSurface` and
    `ReleaseSurface`, to order handlers around.
- **Resources it reads from other plugins:** `*platform.WindowManager`, for
  windows' native handles and framebuffer sizes.
- **Events it publishes:** `DrawEvent`, inside its `RenderEvent` handler;
  `BufferMappedEvent` and `CapturedEvent`, from the job that finds them
  resolved; `DeviceLostEvent` and `DeviceRecoveredEvent`.
- **Events it subscribes to:** `RenderEvent` (`Handlers.Draw`), which draws
  the frame; `platform.WindowOpenedEvent` (`CreateSurface`),
  `platform.ResizeEvent` (`ResizeSurface`) and `platform.WindowClosedEvent`
  (`ReleaseSurface`), which keep a surface per window.
- **Sockets it declares:** `InstanceSocket`, required and exclusive, for the
  backend.
- **Sockets it binds:** none.

gfx requires platform, listed before it, and checks it with
`Dependencies`. metronome goes anywhere. The backend binds `InstanceSocket`,
so it goes anywhere too. A plugin that reads `Handlers` in its `Init` is
listed after gfx.

## Wiring

```go
supervisor.Spawn(map[kernel.PluginName]any{
	metronome.PluginName: &metronome.Config{Tracks: []metronome.Track{
		metronome.NewTrack[gfx.RenderEvent](vsync), // MaxInFlight 1, MinInterval 1/240 s
	}},
}, workerCount,
	gogpu.NewPlugin(), // binds gfx.InstanceSocket, platform's window and input seams
	platform.NewPlugin(),
	metronome.NewPlugin(),
	gfx.NewPlugin(),
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
