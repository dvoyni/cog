# gfx guide

How to use `github.com/dvoyni/cog/gfx`, with examples: drawing a frame,
creating GPU resources, shaders and pipelines, uploads, readback, capture,
device loss, testing without a GPU, and implementing a backend. For the
reasons behind each choice, see [design.md](design.md); for the words it
uses, [CONTEXT.md](../CONTEXT.md). It assumes the
[kernel guide](../../kernel/docs/README.md), the
[metronome guide](../../metronome/docs/README.md) and the
[platform guide](../../platform/docs/README.md). The
[gogpu guide](../../gogpu/docs/README.md) covers running on a real GPU.

gfx follows WebGPU, and wgpu's Go API, one to one in its own types, so
WebGPU's documentation answers what a descriptor field or a command does.
This guide covers what gfx adds and changes: handles instead of objects, the
frame, the events, and the rules that keep them safe under the kernel's
parallel jobs.

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Configuration and lifecycle](#configuration-and-lifecycle)
- [The render track](#the-render-track)
- [Recording a frame](#recording-a-frame)
- [Handles and resources](#handles-and-resources)
- [Shaders](#shaders)
- [Pipelines](#pipelines)
- [Writes](#writes)
- [Surfaces](#surfaces)
- [Readback](#readback)
- [Capture](#capture)
- [Timing signals](#timing-signals)
- [Query sets and timestamps](#query-sets-and-timestamps)
- [Device loss](#device-loss)
- [Errors](#errors)
- [Testing with the recording adapter](#testing-with-the-recording-adapter)
- [Implementing a backend](#implementing-a-backend)
- [Rules](#rules)

## Concepts

```
 gogpu.NewPlugin(), or recording.NewInstance() ──► InstanceSocket
                                                        │
 metronome: RenderEvent, each render beat ───────►     gfx     ◄─── platform: windows,
                                                        │            native handles, sizes
       ┌────────────────────────┬───────────────────────┼──────────────────────────┐
    Device                  DrawEvent              BufferMappedEvent       DeviceLostEvent
 kernel.Read[gfx.Device]   once a frame's          CapturedEvent           DeviceRecoveredEvent
 every GPU operation       framebuffers are        readback, capture       loss and recovery
                           acquired
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. |
| `Config` | gfx's part of the kernel's config: the adapter, features and limits to request, and the surface request. |
| `InstanceSocket` | The backend seam, wgpu's instance mirrored. Required and exclusive: exactly one plugin binds it. |
| `Device` | The resource of the GPU every job shares, read with `kernel.Read[gfx.Device]`. Every GPU operation is one of its methods. |
| `ExclusiveDevice` | The `Device` one job holds alone, from `ElevateDeviceAccess(kernel.Write[gfx.Device])`: `WaitIdle` and `RequestSurface`. No resource. |
| `Buffer`, `Texture`, `TextureView`, `Sampler`, `ShaderModule`, `BindGroupLayout`, `BindGroup`, `PipelineLayout`, `RenderPipeline`, `ComputePipeline`, `QuerySet` | Handles: pointer-free, comparable, generational names of GPU resources. Zero is none. |
| `RenderEvent` | The render track's beat, `struct{ metronome.Beat }`. The application declares the track. |
| `DrawEvent` | Published inside each render beat once the frame's framebuffers are acquired: `Frame`, `Ready`, `AcquireWait`, `GPULatency`. |
| `Frame` | One frame in flight, until its present. |
| `SubmitOrder` | The number a frame encoder is handed under; a frame's encoders are submitted sorted by it. |
| `ShaderBinding` | One binding a shader module declares, by name, as `Device.ShaderModuleBindings` answers it. |
| `CommandEncoder`, `StandaloneCommandEncoder`, `RenderPassEncoder`, `ComputePassEncoder`, `CommandBuffer` | The recording objects, wgpu's. |
| `SurfaceRequest`, `SurfaceConfiguration` | What every window's surface is asked for, and what one window got. |
| `BufferMappedEvent`, `CapturedEvent`, `ReadCapture` | Readback and capture, asynchronous, delivered by events. |
| `DeviceLostEvent`, `DeviceRecoveredEvent` | A device loss, and gfx's recovery from it under the same handles. |
| `Handlers` | gfx's handlers, `Draw`, `CreateSurface`, `ResizeSurface`, `ReleaseSurface`, for others to order theirs around. |
| `Tasks` | The tasks a backend may need on the main thread, for it to pin. gfx schedules them; no one else may. |
| `recording` | The recording adapter: a backend for tests that records every call instead of drawing. |

gfx draws one frame per render beat and knows nothing of meshes, materials,
scenes or the ECS. A backend does the GPU work: the gogpu plugin over our
wgpu fork, or the recording adapter. gfx owns what is the same on every
backend: the handle tables, the frame chain, submit order, surfaces that
follow platform's windows, "auto" layouts, readback, capture, and recovery
from a device loss.

gfx is being built: its [README](../README.md) lists what works today, and
the [gfx map](https://github.com/dvoyni/cog/issues/608) what remains.

## Quick start

A game opens a window, draws a triangle in it every render beat, and quits
once the window closes. Its `Start` publishes an event to open the window
and make the pipeline, off the frame. Each `DrawEvent` records one render
pass that clears the window and draws the triangle once the pipeline is made.
With no update track, the render beat pumps platform's events.

```go
package main

import (
	"embed"
	"errors"
	"io/fs"
	"runtime"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

const gameName kernel.PluginName = "game"

// orderScene is the submit order of the game's one encoder. The numbers are
// the application's; gfx defines none.
const orderScene gfx.SubmitOrder = 100

//go:embed shaders
var embedded embed.FS

// Scene is the game's resource: its window, and what its frames draw with.
type Scene struct {
	Window   platform.Window
	Pipeline gfx.RenderPipeline
}

// loadEvent is published by the game's Start: work is scheduled only in a
// publication.
type loadEvent struct{}

// game opens a window, draws a triangle in it each render beat, and quits
// once the window closes.
type game struct {
	supervisor  *kernel.Supervisor
	openWindow  *kernel.Task[kernel.NoArg, kernel.NoResult]
	loadScene   *kernel.Task[kernel.NoArg, kernel.NoResult]
	drawScene   *kernel.Task[gfx.DrawEvent, kernel.NoResult]
	closeWindow *kernel.Task[platform.Window, kernel.NoResult]
}

func (p *game) Name() kernel.PluginName { return gameName }

// Dependencies names platform, whose Tasks Init reads, and gfx, whose
// device Start's publication uses.
func (p *game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName, gfx.PluginName}
}

func (p *game) Init(c *kernel.Configurator, _ any) error {
	shaders, err := fs.Sub(embedded, "shaders")
	if err != nil {
		return err
	}
	c.BindSocket(platform.ReadMountSocket{FS: shaders})
	c.InitResource(Scene{})
	p.openWindow = c.RegisterTask2(openWindow)
	p.loadScene = c.RegisterTask3(loadScene)
	p.drawScene = c.RegisterTask2(drawScene)
	p.closeWindow = c.RegisterTask1(closeWindow)

	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event loadEvent) (loadEvent, kernel.JobHandle) {
		opened := ex.Schedule(p.openWindow, kernel.NoArg{}, in)
		loaded := ex.Schedule(p.loadScene, kernel.NoArg{}, in)
		return event, ex.Join(opened.Handle(), loaded.Handle())
	})
	// The recording starts once the framebuffers are acquired, and gfx
	// presents once what is joined into the out dependency has finished.
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		return e, ex.Join(in, ex.Schedule(p.drawScene, e, e.Ready).Handle())
	})
	tasks, _ := c.Resource[platform.Tasks]()
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		return beat, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.CloseRequestedEvent) (platform.CloseRequestedEvent, kernel.JobHandle) {
		return event, ex.Schedule(p.closeWindow, event.Window, in).Handle()
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
		p.supervisor.Quit()
		return event, in
	})
	return nil
}

// Start publishes loadEvent. gfx, listed before the game, brought its device
// up in its own Start.
func (p *game) Start(k *kernel.Kernel) error {
	k.Publish(loadEvent{})
	return nil
}

func (p *game) Stop(*kernel.Kernel) error { return nil }
func (p *game) Destroy() error             { return nil }

// openWindow opens the game's window, which opens on the next pump.
func openWindow(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager], scene kernel.Write[Scene]) kernel.NoResult {
	opened := scene.Get()
	opened.Window = manager.Get().Open(platform.WindowEntry{Title: "Triangle", Size: platform.Size{Width: 1280, Height: 720}})
	scene.Set(opened)
	return kernel.NoResult{}
}

// loadScene makes the triangle's pipeline, off the frame: shader modules and
// pipelines are slow to create.
func loadScene(k *kernel.Kernel, _ kernel.NoArg, device kernel.Read[gfx.Device], files kernel.Read[platform.FS], scene kernel.Write[Scene]) kernel.NoResult {
	source, err := device.Get().PreprocessShader(files.Get(), "triangle.wgsl", nil)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	module, err := device.Get().CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "triangle", WGSL: source})
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	// A pipeline keeps what it is made from, so the module goes at once.
	defer device.Get().ReleaseShaderModule(module)
	pipeline, err := device.Get().CreateRenderPipeline(gfx.RenderPipelineDescriptor{
		Label:  "triangle",
		Vertex: gfx.VertexState{Module: module, EntryPoint: "vertexMain"},
		Fragment: gfx.FragmentState{
			Module:     module,
			EntryPoint: "fragmentMain",
			// The format of the default surface request, which every backend has.
			Targets: []gfx.ColorTargetState{{Format: gfx.TextureFormatBGRA8Unorm, WriteMask: gfx.ColorWriteFlagAll}},
		},
	})
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	loaded := scene.Get()
	loaded.Pipeline = pipeline
	scene.Set(loaded)
	return kernel.NoResult{}
}

// drawScene clears the window, and draws the triangle once its pipeline is
// made.
func drawScene(k *kernel.Kernel, e gfx.DrawEvent, device kernel.Read[gfx.Device], scene kernel.Read[Scene]) kernel.NoResult {
	view := device.Get().Framebuffer(e.Frame, scene.Get().Window)
	if view == (gfx.TextureView{}) {
		return kernel.NoResult{} // not open yet, hidden or minimized
	}
	encoder, err := device.Get().Encoder(e.Frame, orderScene)
	if err != nil {
		if !errors.Is(err, gfx.ErrDeviceLost) { // gfx logs a loss itself
			k.Logger().LogErr(err)
		}
		return kernel.NoResult{}
	}
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments: []gfx.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gfx.LoadOpClear,
			StoreOp:    gfx.StoreOpStore,
			ClearValue: gfx.Color{R: 0.05, G: 0.05, B: 0.08, A: 1},
		}},
	})
	if pipeline := scene.Get().Pipeline; pipeline != (gfx.RenderPipeline{}) {
		pass.SetPipeline(pipeline)
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
	}
	pass.End()
	return kernel.NoResult{}
}

// closeWindow closes the window the user asked to close.
func closeWindow(k *kernel.Kernel, window platform.Window, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
	manager.Get().Close(window)
	return kernel.NoResult{}
}

func init() { runtime.LockOSThread() }

func main() {
	// The vsync preset: the Fifo acquire blocks on the display and paces the
	// track; the floor keeps a minimized window from spinning.
	vsync := metronome.Variable()
	vsync.MinInterval = time.Second / 240
	vsync.MaxDelta = 250 * time.Millisecond

	s := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](vsync)},
		},
	}
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0),
		metronome.NewPlugin(),
		platform.NewPlugin(),
		gfx.NewPlugin(),
		gogpu.NewPlugin(), // the backend, windows and input
		&game{supervisor: s},
	); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
}
```

`shaders/triangle.wgsl`, beside `main.go`:

```wgsl
@vertex
fn vertexMain(@builtin(vertex_index) index: u32) -> @builtin(position) vec4<f32> {
	var corners = array<vec2<f32>, 3>(vec2<f32>(0.0, 0.5), vec2<f32>(-0.5, -0.5), vec2<f32>(0.5, -0.5));
	return vec4<f32>(corners[index], 0.0, 1.0);
}

@fragment
fn fragmentMain() -> @location(0) vec4<f32> {
	return vec4<f32>(1.0, 0.5, 0.2, 1.0);
}
```

The pipeline's `Layout` is zero, so gfx gives it an "auto" layout reflected
from the shader: none here, since the shader binds nothing. `Primitive` and
`Multisample` are zero, which is a triangle list, counter-clockwise front
faces, no culling, and one sample.

## Configuration and lifecycle

gfx's part of the kernel's config is a `*gfx.Config`, read in `Init`:

```go
config := map[kernel.PluginName]any{
	gfx.PluginName: &gfx.Config{
		PowerPreference:  gfx.PowerPreferenceHighPerformance,
		RequiredFeatures: gfx.FeatureFlagTextureCompressionBC,
		Surface:          gfx.SurfaceRequest{Format: gfx.TextureFormatBGRA8UnormSrgb},
	},
	// ...
}
```

| Field | What it does |
|---|---|
| `PowerPreference` | Which GPU to ask for when several are present. Zero is none. |
| `ForceFallbackAdapter` | Asks for wgpu's software adapter: real rendering on the CPU, for pixel tests on a machine with no GPU. |
| `RequiredFeatures` | The `FeatureFlags` the device is created with; `Device.Features()` answers them. A feature the adapter lacks fails the device's request, and `Spawn` with it. |
| `RequiredLimits` | The `Limits` the device is created with. Zero is `DefaultLimits()`, WebGPU's defaults. A non-zero `Limits` is taken whole, so start from `DefaultLimits()` and raise the fields you need. |
| `Surface` | The `SurfaceRequest` every window's surface starts with. See [Surfaces](#surfaces). |

```go
limits := gfx.DefaultLimits()
limits.MaxTextureDimension2D = 16384
config[gfx.PluginName] = &gfx.Config{RequiredLimits: limits}
```

**The kernel's order.** gfx requires platform and metronome, listed before
it, and checks it with `Dependencies`. The backend's plugin is listed after
gfx, since it reads `gfx.Tasks` in its `Init`; so is every plugin that
orders its handlers around `gfx.Handlers`. A plugin's place in `Spawn`'s list
is what matters, not its `Dependencies`, which only check it.

**The lifecycle:**

- **`Init`** declares `InstanceSocket`, installs the `Device`, empty until
  `Start`, the `Tasks` and the `Handlers`, and subscribes to `RenderEvent` and
  platform's window events. It makes `DrawEvent`'s stopwatches from
  metronome's `Clock` resource, so gfx times frames by whatever clock
  metronome was given: a `ManualClock` in a test.
- **`Start`** requests the adapter and the device from the bound backend. No
  adapter, or no device, is `Start`'s error, and `Spawn` returns it:
  `gfx: requesting an adapter: …`. From then on the `Device` works, so a
  plugin listed after gfx creates resources from the jobs its own `Start`
  publishes, as the quick start does.
- **`Stop`** schedules the release of the surfaces still open, after the
  last present, then the release of the device, the adapter and the
  instance. Frames in flight finish first; a beat after the release draws
  nothing. Maps and captures still pending are dropped without an event,
  their buffers unmapped and released.
- **`Destroy`** releases only what a `Start` that failed at the device had
  requested.

End the application through the supervisor, `Supervisor.Quit`, so the release
runs: releasing the device cleanly is what saves Vulkan's pipeline cache.

What gfx shares through the kernel, every resource, event and socket, is
listed in the README's [Kernel surface](../README.md#kernel-surface).

## The render track

gfx draws on a track the application declares, with
`metronome.NewTrack[gfx.RenderEvent]`. Its settings choose how frames are
paced, and pair with a present mode:

| Draw mode | Track settings | Present mode |
|---|---|---|
| vsync | `Variable()`, `MinInterval` 1/240 s: `MaxInFlight` 1 plus the floor. | `PresentModeFifo`, the default. The acquire blocks on the display, holding the beat in flight, so the display paces the track. |
| limit(fps) | `MinInterval` 1/fps, plus a `MaxInFlight`. | `PresentModeMailbox` or `PresentModeImmediate`, where the backend offers them. |
| uncapped | A `MaxInFlight` alone. | `PresentModeMailbox` or `PresentModeImmediate`, where the backend offers them. |

Nothing enforces the pairing. gogpu's surfaces offer Fifo alone, so on gogpu
every preset is held to the display's refresh. On Metal, Immediate and
Mailbox are the same, and FifoRelaxed turns vsync off.

**A render track needs a `MaxInFlight`.** gfx keeps a pool of frame slots
that grows only while more beats are in flight than it holds, so the track's
in-flight limit bounds it. One beat in flight, as vsync keeps, takes about a
frame off the gap between a beat's time and the display.

Each render beat, gfx's handler, `Handlers.Draw`, schedules:

```
           after the previous frame's present
                        │
  configure the surfaces marked for it      (write lock; only when one is marked)
                        │
  acquire every drawable window's framebuffer  ─── DrawEvent.Ready
                        │
  DrawEvent: the application's recording tasks, scheduled after Ready
                        │
  flush: finish the frame's encoders, sorted by submit order; submit once;
         copy out the captures; poll the maps
                        │
  present every framebuffer acquired       ─── the beat's out dependency
```

**Frames record one at a time.** A surface holds one acquired framebuffer at
a time, so each frame's acquire waits for the previous frame's present.
Frames overlap on the GPU, through the swapchain, not on the CPU. The beat
stays in flight until its present.

**`DrawEvent` is published every beat**, even when no window has a
framebuffer, so a game with no window open still gets its beat. After a
device loss the beat recovers instead, and publishes no `DrawEvent`; see
[Device loss](#device-loss).

**Ordering around gfx.** Handlers run in plugin order, so a plugin listed
after gfx handles `RenderEvent` after `Draw`, its `in` being the frame's
present. To run one before, order it with the `Sub` from `gfx.Handlers`:

```go
handlers, _ := c.Resource[gfx.Handlers]()
mine := c.Subscribe(p.beforeDraw)
c.OrderSubscriptions(mine, handlers.Draw)
```

## Recording a frame

Subscribe to `DrawEvent` with no ordering. Schedule each recording task
**after `e.Ready`**, with the event as its arg, and return
**`ex.Join(in, own)`**, so gfx's flush waits for it:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
	shadows := ex.Schedule(p.recordShadows, e, e.Ready)
	main := ex.Schedule(p.recordMain, e, e.Ready)
	return e, ex.Join(in, shadows.Handle(), main.Handle())
})
```

- **After `e.Ready`, not after `in`.** `in` is the previous handler's out
  dependency, the recording of every plugin before yours: a task scheduled
  after it waits for theirs, and the frame's recording runs in series.
  `Ready` is the acquire, after which every plugin's tasks record in
  parallel.
- **Joined into the out dependency.** gfx flushes after the nested
  publication's out dependency. A task left out of it races the flush and
  the present: its encoder may be finished while it records, or refused with
  `ErrFrameFlushed`. Never leave one out.

In each task, take an encoder with `Device.Encoder(e.Frame, order)`, and
each window's view with `Device.Framebuffer(e.Frame, window)`:

```go
func recordMain(k *kernel.Kernel, e gfx.DrawEvent, device kernel.Read[gfx.Device], scene kernel.Read[*Scene]) kernel.NoResult {
	view := device.Get().Framebuffer(e.Frame, scene.Get().Window)
	if view == (gfx.TextureView{}) {
		return kernel.NoResult{}
	}
	encoder, err := device.Get().Encoder(e.Frame, orderMain)
	if err != nil {
		// ErrOrderTaken, ErrFrameFlushed or ErrDeviceLost; the encoder
		// records into nothing, so recording on is harmless.
		return kernel.NoResult{}
	}
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments: []gfx.RenderPassColorAttachment{{View: view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore}},
		DepthStencilAttachment: gfx.RenderPassDepthStencilAttachment{
			View:            scene.Get().DepthView,
			DepthLoadOp:     gfx.LoadOpClear,
			DepthStoreOp:    gfx.StoreOpStore,
			DepthClearValue: 1,
		},
	})
	for _, mesh := range scene.Get().Meshes {
		pass.SetPipeline(mesh.Pipeline)
		pass.SetBindGroup(0, mesh.Material, nil)
		pass.SetVertexBuffer(0, mesh.Vertices, 0)
		pass.SetIndexBuffer(mesh.Indices, gfx.IndexFormatUint32, 0)
		pass.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: mesh.IndexCount, InstanceCount: 1})
	}
	pass.End()
	return kernel.NoResult{}
}
```

- **One encoder per task**, not one per draw: wgpu allocates for each, and
  each is finished, sorted and submitted at the flush. One goroutine records
  into an encoder at a time, and a task is one goroutine.
- **Never finish a frame encoder.** gfx's flush finishes each, sorts them by
  submit order and submits them once, before the present.
- **Errors are deferred.** A pass's `End` returns nothing. A recording
  mistake, a stale handle included, fails the encoder: at the flush its
  `Finish` error goes to `LogErr`, that encoder is dropped, and the rest are
  submitted.
- **`Encoder` errors** answer an encoder that records into nothing, as do
  the passes it begins: `ErrOrderTaken` for an order already handed out in
  this frame, `ErrFrameFlushed` for a frame flushed, presented or stale, and
  `ErrDeviceLost`.
- **A window with no framebuffer this frame**, hidden, minimized, not yet
  open, its surface outdated or timed out, answers the zero view. Skip
  drawing to it.
- **A `Frame` is valid until its present.** After the flush `Framebuffer`
  still answers the view; after the present, the zero view.
- **A framebuffer is a render attachment.** Its texture,
  `Device.TextureViewTexture(view)`, is described by the surface's
  configuration, and is a copy source only on a frame being captured: draw
  into it, and use [Capture](#capture) to read it.
- **Several windows** draw in one frame: one acquire job, one flush, a
  present of each. Each has its own framebuffer view.

### Submit order

A frame's encoders are submitted sorted by their `SubmitOrder`, an `int32`,
whichever task or plugin took them. Tasks record in parallel; the GPU runs
the command buffers in order, so a shadow pass at a lower order than the
main pass has its shadow map written before the main pass samples it.

The numbers are one space shared by every plugin that records, and gfx
defines none. An application names them in one place, with gaps for the
passes a plugin adds later:

```go
const (
	orderShadows gfx.SubmitOrder = 100
	orderMain    gfx.SubmitOrder = 200
	orderPost    gfx.SubmitOrder = 300
	orderUI      gfx.SubmitOrder = 900
)
```

Everything between the acquire and the present is passes in submit order.
Post effects and deferred shading need nothing more from gfx: a texture
rendered into by one pass and sampled by a later one gets its barrier from
the backend.

### Standalone encoders

Work outside any frame, a bake, a mip chain, a copy for readback, records
into a standalone encoder from any task holding `kernel.Read[gfx.Device]`.
Its owner finishes and submits it:

```go
encoder, err := device.Get().CreateCommandEncoder(gfx.CommandEncoderDescriptor{Label: "bake"})
if err != nil {
	return err
}
pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{Label: "bake"})
pass.SetPipeline(bake)
pass.SetBindGroup(0, bakeGroup, nil)
pass.Dispatch(64, 64, 1)
pass.End()
commands, err := encoder.Finish() // the first error recorded, if any
if err != nil {
	return err
}
return device.Get().Submit(commands)
```

`DiscardEncoding` throws a recording away. A `CommandBuffer` that will not
be submitted is released with its `Release`. `Submit` submits in the order
given, and polls the maps after, even when it fails.

## Handles and resources

A **handle** names one GPU resource: a small comparable value with no
pointer in it, which fits an event, a task arg, a map key or an ECS
component registered transient. Its zero value names none.

- **A released handle is stale.** Calls that answer an error answer
  `ErrReleased` for it, wrapped with what it was: `ErrReleased: the
  view's texture`, `ErrReleased: the buffer at binding 2`. A recording call
  records it as the encoder's deferred error. A second release logs
  `ErrReleased` and does nothing. A handle is never reused: its slot is,
  under a new generation.
- **A handle survives a device loss.** gfx recreates every object under its
  old handle; see [Device loss](#device-loss).
- **`Index()`** is the handle's slot, for backends to store their native
  object at. It is not for applications.

Every operation is a `Device` method, one pair per type:

| Handle | Create | Release | Answered back |
|---|---|---|---|
| `Buffer` | `CreateBuffer(BufferDescriptor)` | `ReleaseBuffer` | `BufferDescriptor(buffer)` |
| `Texture` | `CreateTexture(TextureDescriptor)` | `ReleaseTexture` | `TextureDescriptor(texture)` |
| `TextureView` | `CreateTextureView(texture, TextureViewDescriptor)` | `ReleaseTextureView` | `TextureViewTexture(view)` |
| `Sampler` | `CreateSampler(SamplerDescriptor)` | `ReleaseSampler` | |
| `ShaderModule` | `CreateShaderModule(ShaderModuleDescriptor)` | `ReleaseShaderModule` | |
| `BindGroupLayout` | `CreateBindGroupLayout(BindGroupLayoutDescriptor)` | `ReleaseBindGroupLayout` | |
| `BindGroup` | `CreateBindGroup(BindGroupDescriptor)` | `ReleaseBindGroup` | |
| `PipelineLayout` | `CreatePipelineLayout(PipelineLayoutDescriptor)` | `ReleasePipelineLayout` | |
| `RenderPipeline` | `CreateRenderPipeline(RenderPipelineDescriptor)` | `ReleaseRenderPipeline` | `RenderPipelineBindGroupLayout(pipeline, group)` |
| `ComputePipeline` | `CreateComputePipeline(ComputePipelineDescriptor)` | `ReleaseComputePipeline` | `ComputePipelineBindGroupLayout(pipeline, group)` |
| `QuerySet` | `CreateQuerySet(QuerySetDescriptor)` | `ReleaseQuerySet` | |

The descriptors answered back are copies, and the zero descriptor once the
resource is released. `CreateQuerySet` answers `ErrQuerySetsUnavailable`
today; see [Query sets and timestamps](#query-sets-and-timestamps).

**Descriptors mirror wgpu's**, with handles in place of objects. An optional
part is a value whose zero means none, never a pointer:

- a zero `Fragment.Module` is no fragment stage, a zero `DepthStencil.Format`
  no depth-stencil state, a zero `Blend` no blending, a zero
  `StripIndexFormat` none;
- a zero `DepthStencilAttachment.View` is no depth-stencil attachment, a zero
  `ResolveTarget` none, a zero `TimestampWrites.QuerySet` none;
- a `BindGroupLayoutEntry` sets exactly one of `Buffer`, `Sampler`,
  `Texture` and `StorageTexture`, and a `BindGroupEntry` exactly one of
  `Buffer`, `Sampler` and `TextureView`; a zero `Size` binds the rest of the
  buffer;
- a zero `PrimitiveState` is the spec default, a zero `MultisampleState` one
  sample, a zero `SamplerDescriptor.Compare` no comparison.

Other zero enums, such as a `LoadOp` or a `FilterMode`, are WebGPU's
"undefined": set them. gfx deep-copies every slice it keeps, so the
caller's slices are free once a call returns.

```go
texture, err := device.CreateTexture(gfx.TextureDescriptor{
	Label:         "albedo",
	Size:          gfx.Extent3D{Width: 256, Height: 256, DepthOrArrayLayers: 1},
	MipLevelCount: 1,
	SampleCount:   1,
	Dimension:     gfx.TextureDimension2D,
	Format:        gfx.TextureFormatRGBA8UnormSrgb,
	Usage:         gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagCopyDst,
})
view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{
	Format:          gfx.TextureFormatRGBA8UnormSrgb,
	Dimension:       gfx.TextureViewDimension2D,
	Aspect:          gfx.TextureAspectAll,
	MipLevelCount:   1,
	ArrayLayerCount: 1,
})
sampler, err := device.CreateSampler(gfx.SamplerDescriptor{
	AddressModeU: gfx.AddressModeRepeat,
	AddressModeV: gfx.AddressModeRepeat,
	AddressModeW: gfx.AddressModeRepeat,
	MagFilter:    gfx.FilterModeLinear,
	MinFilter:    gfx.FilterModeLinear,
	MipmapFilter: gfx.FilterModeLinear,
	LodMaxClamp:  32,
	Anisotropy:   1,
})
layout, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{
	Label: "material",
	Entries: []gfx.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gfx.ShaderStageFlagFragment, Texture: gfx.TextureBindingLayout{
			SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D,
		}},
		{Binding: 1, Visibility: gfx.ShaderStageFlagFragment, Sampler: gfx.SamplerBindingLayout{
			Type: gfx.SamplerBindingTypeFiltering,
		}},
	},
})
material, err := device.CreateBindGroup(gfx.BindGroupDescriptor{
	Label:  "brick",
	Layout: layout,
	Entries: []gfx.BindGroupEntry{
		{Binding: 0, TextureView: view},
		{Binding: 1, Sampler: sampler},
	},
})
```

**Release is immediate.** The GPU keeps what submitted work still uses
until that work finishes, so release a resource once nothing will record it
again, without waiting.

**What an object is made from stays with it.** Releasing a pipeline's shader
modules and layout, a pipeline layout's bind group layouts, a bind group's
layout and resources, or a view's texture, while the object made from them
is held, leaves that object working, through a device loss too. The handle
released is stale to its caller at once; gfx keeps its descriptor for the
object, and hands its slot out again once the last such object is released.
**A buffer a bind group binds is the exception:** release it after the bind
group, since gogpu's wgpu does not keep it for the group.

**The device itself** answers `Features()`, `Limits()` and `AdapterInfo()`:
the features and limits it was created with, and the GPU it runs on.

## Shaders

Shader modules take final WGSL. A WGSL file with directives goes through
`Device.PreprocessShader(files, path, defines)` first, from a task holding
`kernel.Read[gfx.Device]` and `kernel.Read[platform.FS]`:

```go
source, err := device.Get().PreprocessShader(files.Get(), "shaders/lit.wgsl", []gfx.ShaderDefine{
	{Name: "SKINNED"},
	{Name: "MAX_LIGHTS", Value: "16u"},
})
if err != nil {
	return err
}
module, err := device.Get().CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "lit skinned", WGSL: source})
```

The backend handles the directives: gfx passes the call to
`InstanceSocket.PreprocessShader`. On gogpu they are those of
[`gogpu/shader`](../../gogpu/shader):

```wgsl
//#include ./common.wgsl
//#const MAX_LIGHTS=8u

@fragment
fn fragmentMain(input: VertexOutput) -> @location(0) vec4<f32> {
	var color = shade(input, MAX_LIGHTS);
//#if SKINNED & !UNLIT
	color = tint(color);
//#endif
	return color;
}
```

- **A directive** is a line starting with `#`, or `//#` to keep the file
  valid WGSL for an editor.
- **`#include path`** splices a file once, relative to the including file
  when it starts with `./` or `../`, otherwise a path in `files`.
- **`#define NAME`** declares a flag, and **`#const NAME=VALUE`** a WGSL
  `const`. **`#if`**, `#elif`, `#else` and `#endif` keep or blank lines by a
  condition over the flags, with `!`, `&`, `|` and parentheses.
- **Lines keep their numbers**, so the backend's compile errors point into
  the source file.

A **`ShaderDefine`** without a `Value` is a flag, read by `#if`, which never
reaches the WGSL; defining a name no file mentions is harmless. With a
`Value` it overrides the `#const` of that name, and is ignored where no file
declares one. The value is WGSL text, not interpreted: `"16"` and `"16u"`
differ. A name given twice keeps its last entry.

A shader the backend refuses answers an **`ErrShaderSource`**, matched with
`errors.As`:

```go
var refused gfx.ErrShaderSource
if errors.As(err, &refused) {
	// refused.Shader is `shaders/lit.wgsl [MAX_LIGHTS=16u SKINNED]`, the path
	// and its defines sorted; refused.At the offending line first, or the
	// chain of #include lines down to it; refused.Message what went wrong.
}
```

Its `Error()` reads as one line: `shader: "shaders/lit.wgsl [SKINNED]":
… (at shaders/lit.wgsl:12)`. Preprocessing is the instance's work, so it
goes on through a device loss. A tool with no GPU, a shader linter or an
asset baker, calls `gogpu/shader.Preprocess` directly.

**`CreateShaderModule`** compiles the WGSL, and the backend reflects the
module once for the pipelines made from it. Invalid WGSL answers the
backend's error. WGSL is the only language; there is no SPIR-V. Hot reload
is not built yet.

**`ShaderModuleBindings`** answers every binding a module declares, by name,
for code that builds its own layouts and binds values by name:

```wgsl
struct Camera {
	viewProjection: mat4x4<f32>,
	position: vec3<f32>,
}
@group(0) @binding(0) var<uniform> camera: Camera;
@group(1) @binding(0) var albedo: texture_2d<f32>;
```

```go
bindings, err := device.ShaderModuleBindings(module)
// bindings[0]: Name "camera", Group 0, Binding 0,
//   Buffer{Type: BufferBindingTypeUniform, MinBindingSize: 80},
//   Members viewProjection at 0 of 64 bytes and position at 64 of 12,
//   EntryPoints ["vertexMain"], the entry points that read it.
// bindings[1]: Name "albedo", Group 1, Binding 0,
//   Texture{SampleType: TextureSampleTypeFloat, ViewDimension: TextureViewDimension2D},
//   EntryPoints none when no entry point samples it.
```

- **Every declared binding** is answered, in the order the module declares
  them, one no entry point uses included, with no entry points.
- **Its kind** is in exactly one of `Buffer`, `Sampler`, `Texture` and
  `StorageTexture`, as a `BindGroupLayoutEntry` holds it, so a layout entry
  is the binding's kind with a visibility. A buffer's `MinBindingSize` is
  its type's size, a runtime-sized array counting one element, and
  `HasDynamicOffset` is false, left to the layout. An f32 texture is
  `TextureSampleTypeFloat`, a depth one `TextureSampleTypeDepth`.
- **`Members`** are a buffer struct's members, each with its name, offset
  and size as WGSL lays them out.
- **`EntryPoints`** name the entry points that use the binding, directly or
  through the functions they call.

The slices are yours to keep. A released module answers `ErrReleased`. On
gogpu the call answers an error until its named reflection is built.

## Pipelines

A render pipeline takes its stages, vertex buffers, primitive, depth-stencil
and multisample states, and a layout:

```go
pipeline, err := device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
	Label: "lit",
	Vertex: gfx.VertexState{
		Module:     module,
		EntryPoint: "vertexMain", // empty is the module's one vertex entry point
		Buffers: []gfx.VertexBufferLayout{{
			ArrayStride: 32,
			StepMode:    gfx.VertexStepModeVertex,
			Attributes: []gfx.VertexAttribute{
				{Format: gfx.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 0},
				{Format: gfx.VertexFormatFloat32x3, Offset: 12, ShaderLocation: 1},
				{Format: gfx.VertexFormatFloat32x2, Offset: 24, ShaderLocation: 2},
			},
		}},
	},
	Primitive: gfx.PrimitiveState{CullMode: gfx.CullModeBack},
	DepthStencil: gfx.DepthStencilState{
		Format:            gfx.TextureFormatDepth32Float,
		DepthWriteEnabled: true,
		DepthCompare:      gfx.CompareFunctionLess,
	},
	Fragment: gfx.FragmentState{
		Module:     module,
		EntryPoint: "fragmentMain",
		Targets: []gfx.ColorTargetState{{
			Format:    configuration.Format, // from Device.SurfaceConfiguration
			WriteMask: gfx.ColorWriteFlagAll,
		}},
	},
})
```

A compute pipeline takes its module, entry point and layout, and the
module's override constants by name in `Constants`:

```go
cull, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{
	Label:      "cull",
	Module:     module,
	EntryPoint: "cullMain",
	Constants:  map[string]float64{"CULL_DISTANCE": 500},
})
```

### Layouts

**An explicit `Layout`** is a `PipelineLayout` of bind group layouts made
with `CreatePipelineLayout`. Bind groups of those layouts work with every
pipeline that shares them.

**A zero `Layout` is WebGPU's "auto".** gfx asks the backend to reflect the
pipeline's shaders and creates what it answers as explicit layouts: one bind
group layout for each group from 0 to the last the entry points use, a group
they skip answered empty, each binding visible to the stages that use it.
Read them back to make bind groups:

```go
layout := device.RenderPipelineBindGroupLayout(pipeline, 0)
group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: layout, Entries: entries})
```

The pipeline owns its "auto" layouts: they are released with it, so never
release them yourself. `RenderPipelineBindGroupLayout` and
`ComputePipelineBindGroupLayout` answer the zero layout for a stale pipeline
or a group past its last.

### What creation checks

gfx checks a pipeline before creating it, from what it can see:

- **The vertex buffers against the shader's inputs.** The backend reflects
  the vertex entry point's inputs. An input no attribute feeds, none at its
  location or one of another type, is **`ErrVertexMismatch`**, naming both:
  `no vertex attribute at location 2 feeds the shader's input vec2<f32>`, or
  `vertex buffer 0's attribute Uint32x2 at location 1 cannot feed the
  shader's input vec2<f32>`. As in WebGPU, an attribute no input reads is
  fine, and so is a component count differing from the input's. Unsigned
  formats feed `u32` inputs, signed ones `i32`, and every other format,
  normalized ones included, `f32` and `f16`.
- **The layout and vertex buffers against the device's limits:** the
  groups, a binding number, each stage's bindings of each kind, the dynamic
  buffers, the vertex buffers, a stride, an attribute's location, the
  attributes. **`ErrLimitExceeded`** names the limit, as `5 bind groups,
  MaxBindGroups is 4`, before any layout is created.

What needs more of the shader than its reflection, a workgroup's size or the
inter-stage variables, the backend checks, answering its own error.

### Creating off the frame

Creating shader modules and pipelines is **slow and synchronous** on every
backend. Create them from a task of your own under
`kernel.Read[gfx.Device]`, at load or in the background, never from a
frame's recording, and bound how many such tasks run at once:

- a compile holds the device's read lock, so a surface configure, which
  takes the write lock, and the frame behind it wait for it;
- a burst of compiles can take every worker from a frame's recording.

gfx has no pipeline cache. The backend's own disk caches persist on Vulkan
and DX12, under `os.UserCacheDir()/gogpu/`, outside platform's `FS`; Metal
has none. Each create answers its own handle: sharing the pipelines of one
descriptor is the caller's.

## Writes

`Device.WriteBuffer` and `Device.WriteTexture` are the upload path:

```go
vertices, err := device.CreateBuffer(gfx.BufferDescriptor{
	Label: "mesh vertices",
	Size:  uint64(len(data)),
	Usage: gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagCopyDst,
})
if err != nil {
	return err
}
if err := device.WriteBuffer(vertices, 0, data); err != nil {
	return err
}

err = device.WriteTexture(
	gfx.ImageCopyTexture{Texture: texture, Aspect: gfx.TextureAspectAll},
	pixels,
	gfx.ImageDataLayout{BytesPerRow: 256 * 4, RowsPerImage: 256},
	gfx.Extent3D{Width: 256, Height: 256, DepthOrArrayLayers: 1},
)
```

- **The data is copied before the call returns**, so the caller's slice is
  free at once.
- **It lands before every command buffer of the next submit**, whichever
  frame or standalone encoder that submit carries. A write from a frame's
  recording task lands before that frame's passes.
- **Two writes to one region in a frame leave the last for the whole
  frame.** Per-draw data needs a region of its own: a ring you bump-allocate
  from each frame, written once.
- **Each call takes the backend's queue lock**: write a task's whole region
  in one call, not one per draw.
- A buffer write's offset and length are multiples of 4.

Per-draw uniforms in one buffer, each draw's at an offset, through a dynamic
offset:

```go
// The layout's entry: Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true}
stride := device.Limits().MinUniformBufferOffsetAlignment // 256 by default
// staging holds every draw's uniforms, each padded to stride.
if err := device.WriteBuffer(uniforms, 0, staging); err != nil {
	return err
}
for index, sprite := range sprites {
	pass.SetBindGroup(0, spriteGroup, []uint32{uint32(index) * stride})
	pass.Draw(gfx.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstVertex: sprite.FirstVertex})
}
```

gfx keeps no staging belt, uniform ring or pool of transient targets: those
are a renderer's, built on gfx. Reusing a buffer region in the next frame is
safe, since releases and writes are ordered on the queue.

## Surfaces

Every window platform opens gets a **surface**, named by its
`platform.Window`; there is no surface handle. gfx creates it on
`WindowOpenedEvent` from the window's native handle and releases it on
`WindowClosedEvent`, after the last present. A creation that fails goes to
`LogErr` and leaves the window without a surface: it is never drawn.

**One surface request** for the whole application, `SurfaceRequest{Format,
PresentMode}`, from `Config.Surface`. Zero is `TextureFormatBGRA8Unorm`,
which every backend accepts, and `PresentModeFifo`, which every backend has.
Change it at runtime under the write lock:

```go
func useMailbox(k *kernel.Kernel, _ kernel.NoArg, device kernel.Write[gfx.Device]) kernel.NoResult {
	gfx.ElevateDeviceAccess(device).RequestSurface(gfx.SurfaceRequest{PresentMode: gfx.PresentModeMailbox})
	return kernel.NoResult{}
}
```

Every surface is configured again, between frames, from the next frame on.

- **A part the surface cannot honour falls back**, to `BGRA8Unorm` or to
  Fifo, with a `LogErr` at each configure. gogpu's surfaces offer
  `BGRA8Unorm` and Fifo alone, so on gogpu sRGB encoding is always your last
  pass's, and every frame waits for the display. Ask only for what the
  backend offers: under the default `StdLogger`, `LogErr` panics.
- **sRGB** is the last pass's to encode with `BGRA8Unorm`.
  `BGRA8UnormSrgb` is honoured where the backend's surfaces offer it.
- **Every surface is opaque.** HDR is not offered: no backend presents it.

**Configured between frames.** A surface is marked for configuring when it is
new, resized, reported suboptimal, outdated or lost, or the request changes.
The next beat configures it under the write lock, after the last present and
before the acquire. A window that is hidden, as its entry requests, or whose
framebuffer is zero is neither configured nor acquired; it stays marked, and
is configured on the first frame it is drawable again.

**What a window got** is `Device.SurfaceConfiguration(window)`: its format,
present mode, and framebuffer size in pixels; false while it has none. Build
the pipelines that draw into a window against its format. No event says a
surface was configured: a task that holds pipelines for a format compares it
with the window's configuration, and rebuilds off the frame when it changed.

**Acquire results** reach gfx as an `AcquireStatus`, mapped by the backend
from its own errors, and every backend's are handled alike:

| Status | Drawn this frame | Then |
|---|---|---|
| `Acquired` | Yes. | Nothing. |
| `Suboptimal` | Yes. | Configured again before the next frame. |
| `Outdated` | No: the zero view. | Configured again before the next frame. |
| `Lost` | No. | Recreated from the window's native handle, then configured. |
| `Timeout` | No. | Nothing. |
| An error | No. | Logged, and configured again. |

**Windows and frames.** A window opened mid-frame joins the next frame. A
window closing is kept from every acquire still to run, and its surface is
released after the last present. gogpu runs one window on Wayland, and wgpu's
Vulkan backend is not yet safe with two swapchains in one frame.

## Readback

Reading GPU data back is asynchronous only: copy it into a buffer with
`BufferUsageFlagMapRead`, start a map, and wait for **`BufferMappedEvent`**.
There is no blocking map: it would wait for the whole device to go idle.

A map follows the submit that writes the buffer. From a standalone encoder,
map right after the submit:

```go
encoder, err := device.Get().CreateCommandEncoder(gfx.CommandEncoderDescriptor{Label: "picking"})
if err != nil {
	return err
}
encoder.CopyBufferToBuffer(picked, 0, readback, 0, 16)
commands, err := encoder.Finish()
if err != nil {
	return err
}
if err := device.Get().Submit(commands); err != nil {
	return err
}
return device.Get().MapBuffer(readback, gfx.MapModeRead, 0, 16)
```

A copy recorded into a frame is submitted at that frame's flush: map its
buffer from a task of a later frame, after its `e.Ready`, when that submit is
made.

gfx checks its pending maps at every submit and every frame's flush, so a
map resolves at a submit after the one it waits on. Every plugin's handler
sees every `BufferMappedEvent`; read the mapped bytes in a task, after
checking the buffer is yours:

```go
func readPicked(k *kernel.Kernel, event gfx.BufferMappedEvent, device kernel.Read[gfx.Device], picking kernel.Write[Picking]) kernel.NoResult {
	if event.Buffer != picking.Get().Readback {
		return kernel.NoResult{}
	}
	if event.Status != gfx.MapStatusMapped {
		return kernel.NoResult{} // MapStatusFailed: the error went to LogErr
	}
	mapped, err := device.Get().MappedRange(event.Buffer, 0, 16)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	picked := picking.Get()
	picked.Entity = binary.LittleEndian.Uint32(mapped)
	picking.Set(picked)
	if err := device.Get().UnmapBuffer(event.Buffer); err != nil {
		k.Logger().LogErr(err)
	}
	return kernel.NoResult{}
}
```

- **`MappedRange`** answers bytes valid until `UnmapBuffer`: copy out what
  you keep. Its offset is a multiple of 8, its size of 4.
- **A map ended before it resolves**, by `UnmapBuffer` or `ReleaseBuffer`,
  publishes nothing.
- **A kernel with no frames running** calls `Device.Poll()` to resolve the
  maps whose submits have finished, without waiting, from the job that
  maps or a later one. The events are published from the polling job.
- **A failed map** publishes `MapStatusFailed`, its error going to `LogErr`
  first: wrong usage, range or state, or a device loss.
- **At shutdown** maps still pending are dropped without an event.

`MapModeWrite` maps a `MapWrite` buffer to write it; uploads go through
`WriteBuffer` instead.

## Capture

A **capture** is one window's framebuffer copied off the GPU: for
screenshots, and automatic visual checks. Request it from a task holding
`kernel.Read[gfx.Device]`:

```go
if err := device.Get().RequestCapture(window); err != nil {
	// ErrCaptureBusy: one is in flight for this window.
	// ErrCaptureUnsupported: not yet configured, or not 8-bit BGRA or RGBA.
}
```

- **The frame captured is the first whose acquire runs after the request**,
  so a request made while a frame draws captures the next one. gfx copies the
  framebuffer after every submit order, so the capture shows the whole frame.
- **Every backend captures** any configured window of an 8-bit BGRA or RGBA
  format, DX12 and the software adapter included. A backend whose surface
  textures cannot be copied draws the captured frame offscreen, then onto
  the window, so the window still shows what was captured; the cost falls
  only on frames captured.
- **One capture is in flight per window** at a time.

**`CapturedEvent`** delivers it a frame later. `Beat` is the captured
frame's, the moment it shows. Read it with **`gfx.ReadCapture`**, in a task
joined into the handler's out dependency: gfx unmaps and releases the
buffer once that out dependency finishes.

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.CapturedEvent) (gfx.CapturedEvent, kernel.JobHandle) {
	return event, ex.Join(in, ex.Schedule(p.saveCapture, event).Handle())
})

func saveCapture(k *kernel.Kernel, event gfx.CapturedEvent, device kernel.Read[gfx.Device], files kernel.Write[platform.FS]) kernel.NoResult {
	if event.Status != gfx.CaptureStatusCaptured {
		return kernel.NoResult{} // NotAcquired: nothing drawn that frame; Failed: logged
	}
	picture, err := gfx.ReadCapture(device.Get(), event)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	if err := platform.ElevateFileAccess(files).WriteFile("capture.png", encoded.Bytes(), 0o644); err != nil {
		k.Logger().LogErr(err)
	}
	return kernel.NoResult{}
}
```

`ReadCapture` answers an `*image.NRGBA` of the bytes the window displayed:
the rows un-strided, BGRA swapped to RGBA, alpha set opaque, with no colour
conversion. An event of another status than `CaptureStatusCaptured`
answers an error. For your own reading, `Buffer` holds `Height` rows of
`BytesPerRow` bytes, a multiple of 256, in `Format`.

| Status | Meaning |
|---|---|
| `CaptureStatusCaptured` | `Buffer` holds the framebuffer, mapped. |
| `CaptureStatusNotAcquired` | The window acquired no framebuffer that frame: hidden, outdated, timed out. |
| `CaptureStatusFailed` | A copy, map or submit failed, its error in `LogErr`, or the device was lost. |

## Timing signals

`DrawEvent` carries two durations, by metronome's clock, for adaptive
quality:

- **`AcquireWait`** is how long the previous frame's acquire blocked. Under
  Fifo the acquire waits for the display: a long wait is headroom, and a
  short one a frame near its budget. Zero for the first frame. With more
  than one beat in flight, it is the newest acquire finished when the beat
  began.
- **`GPULatency`** is the time from the newest finished frame's submit to
  when gfx first saw it complete, accurate to one check a frame. Zero until a
  frame completes.

```go
// Under Fifo, lower the render scale when frames near their budget, and
// raise it when the display keeps waiting on them.
switch {
case e.AcquireWait < time.Millisecond && quality.Scale > 0.5:
	quality.Scale -= 0.05
case e.AcquireWait > 4*time.Millisecond && quality.Scale < 1:
	quality.Scale += 0.05
}
```

gfx times them with two stopwatches made from metronome's `Clock`:
`gfx.AcquireStopwatchName`, started at each backend acquire, and
`gfx.SubmissionStopwatchName`, running since `Init`. On a `ManualClock` they
are deterministic, and a test makes the next acquire take 3 ms with
`clock.LengthenNext(gfx.AcquireStopwatchName, 3*time.Millisecond)`.

## Query sets and timestamps

WebGPU's query sets are declared and wait on the wgpu fork, which exposes
none yet:

- `QuerySet`, `QuerySetDescriptor`, `QueryTypeOcclusion` and
  `QueryTypeTimestamp`;
- `TimestampWrites` on the pass descriptors, a zero `QuerySet` writing
  nothing;
- `CommandEncoder.ResolveQuerySet` into a `BufferUsageFlagQueryResolve`
  buffer, read back through a map;
- `Device.TimestampPeriod()`, the nanoseconds of one tick;
- `FeatureFlagTimestampQuery`, reported only where timestamps work.

Until then `CreateQuerySet` answers `ErrQuerySetsUnavailable` and the zero
query set on every backend, and no backend reports
`FeatureFlagTimestampQuery`. gfx collects no timings itself: a per-pass
profiler is a plugin's.

## Device loss

A **device loss** is the GPU and every object on it stopping working at
once: a driver reset, a timeout, a removed GPU.

- **Detection.** The backend answers `ErrDeviceLost` from any call that
  answers an error. The first job to see it logs it and publishes
  **`DeviceLostEvent`**, once; pending maps publish `MapStatusFailed`, and
  pending captures `CaptureStatusFailed`.
- **While lost**, no call reaches the backend: every call that answers an
  error answers `ErrDeviceLost`, `PreprocessShader` aside, a release only
  retires its handle, and the frame in flight skips its submit and present.
  No frame is drawn.
- **The next beat recovers** instead of drawing, under the write lock: a new
  device, from a fresh adapter if the old one refuses; every object created
  again **under its old handle**, in dependency order; every surface marked
  for configuring; then **`DeviceRecoveredEvent`**. Captures requested and
  not yet copied stay requested for the frames after it.
- **A recovery that fails** is retried on a later beat, at most once a
  second, with no limit, and logged once per loss.

**Handles stay valid, contents do not.** Pipelines, layouts, bind groups,
samplers and views need nothing. Buffers and textures come back empty: their
owners fill them again from tasks scheduled on `DeviceRecoveredEvent`:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DeviceRecoveredEvent) (gfx.DeviceRecoveredEvent, kernel.JobHandle) {
	return event, ex.Schedule(p.uploadMeshes, kernel.NoArg{}, in).Handle()
})
```

Refill what you uploaded once: meshes, loaded textures, static buffers.
Keep their source, or a way to read it again. Render targets rewritten every
frame need nothing. Metal never reports a device loss, so on macOS a lost
device is never recovered.

## Errors

Errors mirror wgpu's. The `Create*` methods, writes, maps, `MappedRange`,
`StandaloneCommandEncoder.Finish`, `Device.Submit`, `RequestCapture` and
`PreprocessShader` return them, wrapped with context: match with
`errors.Is`, or `errors.As` for `ErrShaderSource`.

| Error | Answered by | Means |
|---|---|---|
| `ErrReleased` | Any call with an error given a released handle. | The handle is stale. |
| `ErrDeviceLost` | Every call with an error, `PreprocessShader` aside, from the loss to the recovery. | The device is lost. |
| `ErrOrderTaken` | `Encoder`. | Another encoder of this frame has that submit order. |
| `ErrFrameFlushed` | `Encoder`. | The frame is flushed, presented or stale. |
| `ErrVertexMismatch` | `CreateRenderPipeline`. | A vertex input no attribute feeds. |
| `ErrLimitExceeded` | Both pipeline creations. | A layout or vertex buffers past the device's limits. |
| `ErrCaptureBusy` | `RequestCapture`. | A capture of that window is in flight. |
| `ErrCaptureUnsupported` | `RequestCapture`. | The window is not yet configured, or not 8-bit BGRA or RGBA. |
| `ErrQuerySetsUnavailable` | `CreateQuerySet`. | The wgpu fork exposes no query sets yet. |
| `ErrShaderSource` | `PreprocessShader`. | The backend refused a shader; it names the file and line. |

**What happens inside a frame goes to `LogErr`**, and gfx carries on: a
pass's errors and a frame encoder's, at the flush, the encoder dropped and
the rest submitted; a frame's submit, an acquire or a present that fails; a
surface's creation or configure that fails, and a surface request it cannot
honour; a map the backend fails; a device loss, once; a second release; a
failed recovery, once per loss.

Under `kernel.NewSupervisor(nil)` the `StdLogger` panics on `LogErr`, so each
of these ends the process, which suits development. A shipped game gives the
supervisor a `LogHandler` of its own; see the kernel guide's
[Errors and logging](../../kernel/docs/README.md#errors-and-logging).

**Composition errors panic** at `Spawn`: no `InstanceSocket` bound, or two;
gfx listed before platform or metronome; a config of another type than
`*gfx.Config`.

## Testing with the recording adapter

The **recording adapter**, `github.com/dvoyni/cog/gfx/recording`, is a
backend that draws nothing and records every call gfx makes, as typed
values, for a test to assert on. A test runs gfx in a real kernel: its own
plugin binds the adapter and platform's headless windows, and metronome
paces the render track on a `ManualClock`.

```go
// testBackend binds the recording adapter and headless windows: the backend
// and window system a test runs gfx on.
type testBackend struct {
	instance *recording.Instance
	windows  *headless.Windows
}

func (*testBackend) Name() kernel.PluginName          { return "test backend" }
func (*testBackend) Dependencies() []kernel.PluginName { return nil }

func (b *testBackend) Init(c *kernel.Configurator, _ any) error {
	c.BindSocket[gfx.InstanceSocket](b.instance)
	c.BindSocket[platform.WindowsSocket](b.windows)
	return nil
}

func (*testBackend) Start(*kernel.Kernel) error { return nil }
func (*testBackend) Stop(*kernel.Kernel) error  { return nil }
func (*testBackend) Destroy() error             { return nil }
```

The test spawns the game against it, and steps the clock while ticking the
main thread, where platform's pump runs:

```go
func TestTheGameDrawsItsTriangle(t *testing.T) {
	instance := recording.NewInstance()
	clock := metronome.NewManualClock()
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Clock:  clock,
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](metronome.Variable())},
		},
	}
	if _, err := supervisor.Spawn(config, 2,
		metronome.NewPlugin(),
		platform.NewPlugin(),
		gfx.NewPlugin(),
		&testBackend{instance: instance, windows: headless.NewWindows()},
		&game{supervisor: supervisor},
	); err != nil {
		t.Fatal(err)
	}

	// The window, its surface and the pipeline are made outside the beats,
	// so step until a frame draws.
	for beat := 0; beat < 20; beat++ {
		advance(supervisor, clock, 16*time.Millisecond)
		frames := instance.Recording().Frames()
		if len(frames) == 0 || len(frames[len(frames)-1].Submissions) == 0 {
			continue // no window drawn yet
		}
		frame := frames[len(frames)-1]
		pass := frame.Submissions[0].Buffers[0].Commands[0]
		if len(pass.Commands) < 2 {
			continue // the pipeline is not made yet: the pass only clears
		}
		draw := pass.Commands[1]
		if draw.Kind != recording.CommandKindDraw || draw.Arguments != (gfx.DrawArgs{VertexCount: 3, InstanceCount: 1}) {
			t.Fatalf("the pass recorded %+v, want a Draw of 3 vertices", draw)
		}
		return
	}
	t.Fatal("no frame drew in 20 beats")
}

// advance steps the clock by d on a goroutine of its own, ticking the main
// thread for the pump and any task pinned to it, until the beats it let out
// have finished.
func advance(supervisor *kernel.Supervisor, clock *metronome.ManualClock, d time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		clock.Advance(d)
	}()
	for {
		supervisor.TickMainThread()
		select {
		case <-done:
			return
		default:
			runtime.Gosched()
		}
	}
}
```

**What it records.** `instance.Recording()` answers a copy of what was
recorded so far; read it once the beats have settled.

- **`Calls`**: every create and release with its handle and descriptor,
  writes and maps, surface calls with the status an acquire answered,
  preprocessing, each with a `Kind` and typed `Arguments`, such as a
  `recording.CreateBufferArguments`. Calls from parallel tasks come in no
  fixed order: look them up by handle.
- **`Submissions`**: each submit's command buffers, each a list of
  `Command`s: a pass is a `CommandKindBeginRenderPass` or
  `CommandKindBeginComputePass` with its descriptor as `Arguments`, and its
  own commands, `CommandKindSetPipeline`, `CommandKindDraw` and the rest,
  under `Commands`. Submissions and surface calls come in a deterministic
  order.
- **`Frames()`** groups them by frame: each window's acquire status, the
  submissions, and the presents, from the frame's first acquire to its last
  present. A frame with no window acquired is its one submission.

**What it answers.** It answers what a real backend would, and checks a
fixed subset of what wgpu rejects: a handle missing at its index, usage
flags, write, copy and clear alignment, and the pass sequence.

- **Buffers hold bytes**, which writes, buffer copies and clears change, and
  which its own maps read. Textures hold no texels, and captures complete
  zero-filled.
- **Every feature but `FeatureFlagTimestampQuery`**, and `DefaultLimits()`.
  A device requiring a feature it lacks is refused.
- **Surfaces** offer `recording.SurfaceCapabilities`: the 8-bit BGRA and
  RGBA formats, sRGB or not, and every present mode, more than gogpu's.
- **Reflection** answers no layouts, no vertex inputs and no bindings,
  unless a test queues them, so an "auto" layout is empty, no vertex check
  fails and a module declares no binding.
- **Submissions** are indexed from one on each device and finish at once,
  so `GPULatency` is zero.
- **Preprocessing** answers the file's text as it is.

**Injection.** `instance.AnswerNext(kind, answer)` makes the next call of a
kind answer something else, in the order queued:

```go
// The next acquire answers Outdated: the window draws nothing that frame.
instance.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusOutdated)})
// The next submit loses the device.
instance.AnswerNext(recording.CallKindSubmit, recording.Answer{Error: gfx.ErrDeviceLost})
// The next map fails: MapStatus is never recorded, only answered.
instance.AnswerNext(recording.CallKindMapStatus, recording.Answer{Error: errors.New("refused")})
// The next surface offers what gogpu's do.
instance.AnswerNext(recording.CallKindSurfaceCapabilities, recording.Answer{Capabilities: gfx.SurfaceCapabilities{
	Formats:      []gfx.TextureFormat{gfx.TextureFormatBGRA8Unorm},
	PresentModes: []gfx.PresentMode{gfx.PresentModeFifo},
	AlphaModes:   []gfx.CompositeAlphaMode{gfx.CompositeAlphaModeOpaque},
}})
// The next "auto" layout reflects one group of one uniform buffer.
instance.AnswerNext(recording.CallKindReflectRenderPipelineLayout, recording.Answer{BindGroupLayouts: []gfx.BindGroupLayoutDescriptor{{
	Entries: []gfx.BindGroupLayoutEntry{{Binding: 0, Visibility: gfx.ShaderStageFlagVertex, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}}},
}}})
// The next module's named reflection is one uniform block.
instance.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: []gfx.ShaderBinding{{
	Name:        "camera",
	Buffer:      gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 64},
	Members:     []gfx.ShaderStructMember{{Name: "viewProjection", Offset: 0, Size: 64}},
	EntryPoints: []string{"vertexMain"},
}}})
```

**Real pixels** come from gogpu on wgpu's software adapter, with
`gfx.Config{ForceFallbackAdapter: true}`, not from the recording adapter.
Test what records in parallel under `go test -race`.

## Implementing a backend

A backend is a plugin that binds `InstanceSocket` in its `Init`, listed after
gfx, as gogpu does. Exactly one binding is allowed. gfx calls it from `Start`
on, from whichever worker runs the job.

```go
func (p *Plugin) Init(c *kernel.Configurator, _ any) error {
	c.BindSocket[gfx.InstanceSocket](instance{plugin: p})
	// Pin the tasks whose backend calls this platform runs on the main thread.
	tasks, _ := c.Resource[gfx.Tasks]()
	c.PinToMainThread(tasks.AcquireFramebuffers)
	c.PinToMainThread(tasks.PresentFramebuffers)
	return nil
}
```

The seam is wgpu's, in gfx's types:

- **`InstanceSocket`**: `RequestAdapter`, `CreateSurface` and
  `ReleaseSurface` from a window's native handle, `PreprocessShader`, and
  `Release`. A window whose native handle is zero is a headless one: its
  surface takes no window.
- **`BackendAdapter`** answers its `Info`, `Features` and `Limits`, and
  `RequestDevice`.
- **`BackendDevice`** has `Device`'s methods, each given the handle gfx
  allocated.

What a backend owes gfx:

- **Native objects live at `handle.Index()`.** gfx allocates every handle,
  checks its generation and keeps its descriptor; it has checked every handle
  in a call before making it. Descriptors and recording calls reach the
  backend as gfx's own, so nothing is translated. The enums are numbered as
  gputypes numbers them, and convert by cast.
- **Errors map onto gfx's.** A device loss answers `ErrDeviceLost`, as
  `errors.Is` sees it, from any call that answers an error: Vulkan's
  device-lost result, DX12's removed or reset device. An acquire answers an
  `AcquireStatus`.
- **Reflection at module creation.** `CreateShaderModule` parses and
  reflects the WGSL once, keeping the result at the module's index for
  `ReflectRenderPipelineLayout`, `ReflectComputePipelineLayout`,
  `ReflectVertexInputs` and `ReflectBindings`. `ReflectBindings` answers
  every binding the module declares, used or not, with its name, its
  struct's members and the entry points that use it, in slices the backend
  keeps no part of. `CreateRenderPipeline` and `CreateComputePipeline`
  always get an explicit layout.
- **Submissions are counted.** `Submit` answers the submission's index,
  from one on each device; `Poll` resolves finished maps without waiting and
  answers the newest index finished. `MapStatus` answers whether a map has
  resolved, or the error that ended it.
- **Surfaces** answer their `SurfaceCapabilities`, are configured opaque by
  `ConfigureSurface` under the write lock, between a present and the next
  acquire, and `AcquireFramebuffer` stores the framebuffer's texture and
  view at the indices gfx hands it, until `Present`. A captured acquire's
  texture is a copy source: a backend whose surface textures cannot be copied
  stores an offscreen texture of the surface's format and draws it onto the
  surface at the present.
- **`BackendDevice.Release` releases everything.** gfx releases nothing on
  a lost device, so the device's `Release` releases every object it still
  holds, leaves each surface unconfigured, and drops a framebuffer acquired
  and not presented. A recovery calls it before asking the adapter for a new
  device.

**Threads are the backend's.** gfx pins none of its jobs. The `Tasks`
resource holds the ones that make surface calls, `CreateSurface`,
`ReleaseSurface`, `ConfigureSurfaces`, `AcquireFramebuffers`,
`PresentFramebuffers` and `ReleaseOpenSurfaces`, the device's release at
teardown, `ReleaseDevice`, and its recovery, `RecoverDevice`. A backend pins
those its platform needs on the main thread, and schedules none of them:
gfx does.

## Rules

- **Record from tasks scheduled after `DrawEvent.Ready`**, joined into the
  handler's out dependency, and never after `in`.
- **One encoder per task that records in parallel**, not one per draw, and
  never `Finish` a frame encoder.
- **Name the submit orders in one place**, with gaps: the space is shared by
  every plugin.
- **Create shader modules and pipelines off the frame**, from tasks of your
  own, bounding how many run at once.
- **Write a task's region once per frame.** Per-draw data gets a region of
  its own.
- **Never block a job on the GPU.** Map, and wait for `BufferMappedEvent`.
- **Read the `Device` in each task**, through `kernel.Read[gfx.Device]`, and
  keep handles, never the `Device`.
- **Refill buffers and textures on `DeviceRecoveredEvent`.** Handles survive
  a loss; contents do not.
- **Ask the surface only for what the backend offers**, and build a window's
  pipelines against its `SurfaceConfiguration`.
- **Start custom limits from `DefaultLimits()`.**
