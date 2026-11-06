# gfx guide

How to use `github.com/dvoyni/cog/gfx`. For the reasons behind each choice,
see [design.md](design.md). It assumes the
[kernel guide](../../kernel/docs/README.md), the
[metronome guide](../../metronome/docs/README.md) and the
[platform guide](../../platform/docs/README.md). gfx is being built: the
names below are the module's; the device comes up, and the other bodies panic
until they are filled.

- [Concepts](#concepts)
- [The render track](#the-render-track)
- [Recording a frame](#recording-a-frame)
- [Resources](#resources)
- [Shaders and pipelines](#shaders-and-pipelines)
- [Writes](#writes)
- [Surfaces](#surfaces)
- [Readback and capture](#readback-and-capture)
- [Device loss](#device-loss)
- [Testing with the recording adapter](#testing-with-the-recording-adapter)
- [Rules](#rules)

## Concepts

```
gogpu, or recording.NewInstance() ──  InstanceSocket
                                         │
metronome: RenderEvent ──────────────►  gfx  ◄── platform: windows
                                         │
                    Device, Handlers, DrawEvent, the other events
```

- **Device:** the GPU every job shares, under `kernel.Read[gfx.Device]`.
- **Handle:** a pointer-free name for one GPU resource; zero is none.
- **Frame:** what one beat draws: a framebuffer per open window, and the
  encoders recorded into it.
- **Submit order:** the number a frame encoder is handed under; a frame's
  encoders are submitted sorted by it, across every plugin.

## The render track

The application declares the render track with
`metronome.NewTrack[gfx.RenderEvent]`. The vsync preset is
`MaxInFlight: 1` with a 1/240 s `MinInterval`, paired with the Fifo present
mode: the acquire blocks on the display and paces the track. gfx's handler on
`RenderEvent` acquires every window's framebuffer, publishes `DrawEvent`,
then flushes and presents. A render track needs a `MaxInFlight`.

Give gfx the clock given to metronome, `Config.Clock`, so `DrawEvent`'s
`AcquireWait` and `GPULatency` follow a test's manual clock.

## Recording a frame

Subscribe to `DrawEvent` with no ordering. Schedule each recording task after
`e.Ready`, with the event as its arg, and return `ex.Join(in, own)` so gfx's
flush waits for it:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
	shadows := ex.Schedule(recordShadows, e, e.Ready)
	main := ex.Schedule(recordMain, e, e.Ready)
	return e, ex.Join(in, shadows.Handle(), main.Handle())
})
```

In the task, take one encoder per task with `Device.Encoder(e.Frame, order)`,
and the window's view with `Device.Framebuffer(e.Frame, window)`. Tasks record
in parallel; the GPU runs them in submit order, so `orderShadows` below
`orderMain` makes the shadow map ready for the main pass. The numbers are the
application's: gfx defines none. Never `Finish` a frame encoder: gfx does.

A window with no framebuffer this frame, hidden or minimized, answers the zero
view; skip drawing to it. A `Frame` is valid until its present.

Off the frame, `Device.CreateCommandEncoder` answers a standalone encoder:
finish it and pass its buffer to `Device.Submit`.

## Resources

Create and release them with `Device`: `CreateBuffer`/`ReleaseBuffer`,
`CreateTexture`, `CreateTextureView`, and the rest, one pair per type. An
optional part of a descriptor is a value whose zero means none. Release is
immediate; the GPU keeps what in-flight work still uses.

## Shaders and pipelines

Preprocess WGSL with `PreprocessShader(files, path, defines)` under your own
`kernel.Read[platform.FS]`, then `CreateShaderModule`. Leave a pipeline's
`Layout` zero for an "auto" layout reflected from its shaders, and read its
groups back with `RenderPipelineBindGroupLayout`.

Creating modules and pipelines is slow and synchronous. Do it in a task of
your own, at load or in the background, and bound how many such tasks run at
once: a burst can take every worker from a frame, and a compile delays a
surface's reconfiguring. Vulkan and DX12 cache pipelines on disk by
themselves.

## Writes

`Device.WriteBuffer` and `WriteTexture` are the upload path. The data is
copied before the call returns, and lands before every command buffer of the
next submit. Two writes to one region in a frame leave the last for the
whole frame, so per-draw data needs its own region: a ring you bump-allocate
from each frame, written once per task.

## Surfaces

Every window gets a surface. `Config.Surface`, a `SurfaceRequest`, says what
they are configured with; zero is `BGRA8Unorm` and Fifo, so encode sRGB in
your last pass. Change it at runtime under the write lock:

```go
gfx.ElevateDeviceAccess(device).RequestSurface(gfx.SurfaceRequest{PresentMode: gfx.PresentModeMailbox})
```

Every surface is reconfigured between frames. Read what a window got with
`Device.SurfaceConfiguration(window)`, and build its pipelines against that
format.

## Readback and capture

Copy what you want into a `MapRead` buffer, call `Device.MapBuffer`, and wait
for `BufferMappedEvent`. Read `MappedRange` in a task, then `UnmapBuffer`.
Nothing blocks.

To capture a window, set `SurfaceRequest.Capturable` (not on DX12 yet), call
`Device.RequestCapture(window)`, and read the frame a frame later from
`CapturedEvent` with `gfx.ReadCapture`, in a task joined into the handler's
out dependency. `event.Beat` says which moment it shows.

## Device loss

On `DeviceLostEvent`, nothing is drawn until `DeviceRecoveredEvent`. Your
handles stay valid; gfx recreates every object under them, empty. On
`DeviceRecoveredEvent`, refill what you uploaded: meshes, loaded textures,
static buffers. Render targets are rewritten every frame and need nothing.

## Testing with the recording adapter

Bind `recording.NewInstance()` in the test's plugin, with platform's headless
windows adapter and metronome on a `ManualClock`. Step the clock, let the
kernel settle, and assert on `instance.Recording()`: its `Frames()`, their
submissions in order, each pass's commands. Make the next acquire answer
`Outdated`, or any call answer `gfx.ErrDeviceLost`, with
`instance.AnswerNext`. For real pixels, run gogpu on the software adapter
with `Config.ForceFallbackAdapter`.

## Rules

- Record only from tasks scheduled after `DrawEvent.Ready`, joined into the
  handler's out dependency.
- One encoder per task that records in parallel, not one per draw.
- Create pipelines off the frame.
- Write a task's region once per frame.
- Never block in a job on the GPU: map, and wait for the event.
