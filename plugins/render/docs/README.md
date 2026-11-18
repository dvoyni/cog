# render guide

How to use `github.com/dvoyni/cog/plugins/render`, with examples: loading and making
assets, materials and their parameters, recording a frame in command lists,
passes and draws, compute, copies and temporaries, the frame's other work,
and testing a plugin that draws. For the reasons behind each choice, see
[design.md](design.md); for the words it uses, [CONTEXT.md](../CONTEXT.md).
It assumes the [kernel guide](../../../kernel/docs/README.md), the
[metronome guide](../../metronome/docs/README.md), the
[platform guide](../../platform/docs/README.md) and the
[gfx guide](../../gfx/docs/README.md).

A plugin that draws uses render alone: it names no gfx type and takes no gfx
lock.

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [The plugin and its Config](#the-plugin-and-its-config)
- [The frame](#the-frame)
- [Loading and sharing assets](#loading-and-sharing-assets)
- [Made assets and writes](#made-assets-and-writes)
- [Shaders and variants](#shaders-and-variants)
- [Materials](#materials)
- [Command lists](#command-lists)
- [Passes](#passes)
- [Draws, instances and pass state](#draws-instances-and-pass-state)
- [Pass and draw parameters](#pass-and-draw-parameters)
- [Compute passes](#compute-passes)
- [Copies and buffer clears](#copies-and-buffer-clears)
- [Temporaries](#temporaries)
- [Mips](#mips)
- [Capture](#capture)
- [Readback](#readback)
- [The surface request](#the-surface-request)
- [Device recovery](#device-recovery)
- [Inspections](#inspections)
- [Pass timings](#pass-timings)
- [Hot reload](#hot-reload)
- [Reports and errors](#reports-and-errors)
- [Testing with rendertest](#testing-with-rendertest)
- [Rules](#rules)

## Concepts

```
 gfx.DrawEvent, once a frame's framebuffers are acquired
        │
      render ── FrameEvent{Ready, AcquireWait, GPULatency}
        │          │
        │          └─ your recording tasks, after Ready, joined into the out dependency
        │                 Frame: BeginCommandList(label, order)
        │                   CommandList: BeginPass, BeginComputePass, copies, readbacks, temporaries
        │                     Pass: SetMaterial, SetPass…, Set…, Draw, DrawVertices
        │
        ├─ Inventory: acquire, make, write and release assets; ReloadChanged
        ├─ Surface: Request(SurfaceRequest)
        └─ RecoveredEvent, CapturedEvent, ReadbackEvent, InspectedEvent, PassTimingsEvent
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. |
| `Config` | render's part of the kernel's config: `TemporaryIdleFrames` and `TemporaryChunkSize`. |
| `Handlers` | render's handlers, for others to order theirs around. |
| `FrameEvent` | The frame render's users record, published inside each of gfx's `DrawEvent`s. |
| `Frame` | The task parameter a recording task reaches the frame through: command lists, temporary textures, capture, readback, inspections and pass timings. |
| `Inventory` | The task parameter a task reaches render's assets through. |
| `Surface` | The task parameter a task changes the windows' surfaces through. |
| `Texture`, `Buffer`, `Shader`, `Mesh`, `Material`, `Readback` | Handles: pointer-free, comparable, an ECS component can hold one. Zero is none. |
| `TextureOptions`, `ShaderDefine`, `BufferUsageFlags` | What a loaded texture, shader or buffer is acquired with, beside its path. |
| `TextureDescriptor`, `MeshDescriptor`, `MaterialDescriptor` | What a made texture, mesh or material is made of. |
| `State` | A material's render state: blend, write masks, depth, stencil, culling. Its zero value is WebGPU's defaults. |
| `TextureView`, `SamplerDescriptor`, `BufferRange` | Parameter values beside a whole texture and a Go value of a uniform block. |
| `CommandList`, `Order` | One task's run of passes, compute passes, copies and readbacks, and the number placing it among the frame's others. |
| `Pass`, `PassDescriptor`, `ColorTarget`, `ResolveTarget`, `DepthStencilTarget` | A run of draws into the targets it declares. |
| `ComputePass` | A run of dispatches. |
| `DrawArgs`, `InstanceBuffer`, `Viewport`, `ScissorRectangle` | A draw's instances, a vertex buffer read per instance, and pass state. |
| `TextureRegion` | A rectangle of one mip level and one layer of a texture, for writes, copies and readbacks. |
| `SurfaceRequest` | What every window's surface is asked for at run time. |
| `Inspection` | One frame written down in render's words. |
| `PassTiming` | How long one pass took on the GPU. |
| `Report`, `ReportKind` | A mistake render finds at run time, and its kind. |
| `rendertest.Harness` | render in a real kernel on a fake GPU, or on wgpu's software adapter, for tests. |

Words used throughout: a **loaded asset** is read from a path in platform's
files and shared by its **asset key**; a **made asset** is built from memory
and released once by its maker; a **parameter** is a value bound to a
shader's binding by its WGSL name; a **temporary** lives for one frame.

## Quick start

A game opens a window and draws a textured quad in it every render beat. Its
plugin mounts the files `main` hands it, and its `Start` publishes an event
whose tasks open the window and load the scene: a shader and a texture from
those files, a mesh made from two slices, and a material binding the texture
by name. Each `FrameEvent` records one command list of one pass that clears
the window and draws the quad. With no update track, the render beat pumps
platform's events.

```go
package main

import (
	"embed"
	"io/fs"
	"runtime"
	"time"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gogpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

const gameName kernel.PluginName = "game"

// orderScene places the game's command list among the frame's. The orders
// are the application's; render keeps only math.MinInt32 and math.MaxInt32.
const orderScene render.Order = 100

//go:embed assets
var embedded embed.FS

// vertex is one corner of the quad, as quad.wgsl reads it.
type vertex struct {
	Position [2]float32
	UV       [2]float32
}

// Scene is the game's resource: its window, and what its frames draw.
type Scene struct {
	Window   platform.Window
	Quad     render.Mesh
	Material render.Material
}

// loadEvent is published by the game's Start: work is scheduled only in a
// publication.
type loadEvent struct{}

// game opens a window and draws a textured quad in it each render beat,
// from files, its read mount.
type game struct {
	supervisor  *kernel.Supervisor
	files       fs.FS
	openWindow  *kernel.Task[kernel.NoArg, kernel.NoResult]
	loadScene   *kernel.Task[kernel.NoArg, kernel.NoResult]
	drawScene   *kernel.Task[render.FrameEvent, kernel.NoResult]
	closeWindow *kernel.Task[platform.Window, kernel.NoResult]
}

func (p *game) Name() kernel.PluginName { return gameName }

// Dependencies names platform, whose Tasks Init reads, and render, whose
// frame the game records.
func (p *game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName, render.PluginName}
}

func (p *game) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: p.files})
	configurator.InitResource(Scene{})
	p.openWindow = configurator.RegisterTask2(openWindow)
	p.loadScene = configurator.RegisterTask2(loadScene)
	p.drawScene = configurator.RegisterTask2(drawScene)
	p.closeWindow = configurator.RegisterTask1(closeWindow)

	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event loadEvent) (loadEvent, kernel.JobHandle) {
		opened := ex.Schedule(p.openWindow, kernel.NoArg{}, in)
		loaded := ex.Schedule(p.loadScene, kernel.NoArg{}, opened.Handle())
		return event, loaded.Handle()
	})
	// The recording starts once render has readied the frame, and gfx
	// presents once what is joined into the out dependency has finished.
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(p.drawScene, event, event.Ready).Handle())
	})
	tasks, _ := configurator.Resource[platform.Tasks]()
	processEvents := tasks.ProcessEvents
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		return beat, ex.Join(in, ex.Schedule(processEvents, kernel.NoArg{}).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.CloseRequestedEvent) (platform.CloseRequestedEvent, kernel.JobHandle) {
		return event, ex.Schedule(p.closeWindow, event.Window, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
		p.supervisor.Quit()
		return event, in
	})
	return nil
}

func (p *game) Start(k *kernel.Kernel) error {
	k.Publish(loadEvent{})
	return nil
}

func (p *game) Stop(*kernel.Kernel) error { return nil }
func (p *game) Destroy() error             { return nil }

// openWindow opens the game's window, which opens on the next pump.
func openWindow(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager], scene kernel.Write[Scene]) kernel.NoResult {
	opened := scene.Get()
	opened.Window = manager.Get().Open(platform.WindowEntry{Title: "Quad", Size: platform.Size{Width: 1280, Height: 720}})
	scene.Set(opened)
	return kernel.NoResult{}
}

// loadScene loads the quad's shader and texture, makes its mesh and
// material. Each exists when its call returns.
func loadScene(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, scene kernel.Write[Scene]) kernel.NoResult {
	shader := inventory.AcquireShader("quad.wgsl")
	crate := inventory.AcquireTexture("crate.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB, Mipmaps: true})
	vertices := inventory.MakeBuffer("quad vertices", render.BufferUsageFlagVertex, []vertex{
		{Position: [2]float32{-0.5, -0.5}, UV: [2]float32{0, 1}},
		{Position: [2]float32{0.5, -0.5}, UV: [2]float32{1, 1}},
		{Position: [2]float32{0.5, 0.5}, UV: [2]float32{1, 0}},
		{Position: [2]float32{-0.5, 0.5}, UV: [2]float32{0, 0}},
	})
	indices := inventory.MakeBuffer("quad indices", render.BufferUsageFlagIndex, []uint16{0, 1, 2, 0, 2, 3})
	quad := inventory.MakeMesh(render.MeshDescriptor{
		Label: "quad",
		VertexBuffers: []render.VertexBuffer{{
			Range: render.BufferRange{Buffer: vertices},
			Layout: render.VertexLayout{Stride: 16, Attributes: []render.VertexAttribute{
				{Format: render.VertexFormatFloat32x2, Offset: 0, Location: 0},
				{Format: render.VertexFormatFloat32x2, Offset: 8, Location: 1},
			}},
		}},
		Indices: render.IndexBuffer{Range: render.BufferRange{Buffer: indices}, Format: render.IndexFormatUint16},
	})
	material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "crate", Shader: shader})
	inventory.SetMaterialTexture(material, "picture", crate)
	inventory.SetMaterialSampler(material, "picture_sampler", render.SamplerDescriptor{
		MagFilter: render.FilterModeLinear, MinFilter: render.FilterModeLinear, MipmapFilter: render.FilterModeLinear,
	})

	loaded := scene.Get()
	loaded.Quad = quad
	loaded.Material = material
	scene.Set(loaded)
	return kernel.NoResult{}
}

// drawScene clears the window and draws the quad, once both exist.
func drawScene(k *kernel.Kernel, event render.FrameEvent, frame render.Frame, scene kernel.Read[Scene]) kernel.NoResult {
	window := scene.Get().Window
	if window == (platform.Window{}) || scene.Get().Material == (render.Material{}) {
		return kernel.NoResult{}
	}
	list := frame.BeginCommandList("scene", orderScene)
	pass := list.BeginPass(render.PassDescriptor{
		Label: "scene",
		ColorTargets: []render.ColorTarget{{
			Window: window, Clear: true, ClearColor: render.Color{R: 0.05, G: 0.05, B: 0.08, A: 1},
		}},
	})
	pass.SetMaterial(scene.Get().Material)
	pass.Draw(scene.Get().Quad, render.DrawArgs{})
	pass.End()
	list.End()
	return kernel.NoResult{}
}

// closeWindow closes the window the user asked to close.
func closeWindow(k *kernel.Kernel, window platform.Window, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
	manager.Get().Close(window)
	return kernel.NoResult{}
}

func init() { runtime.LockOSThread() }

func main() {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	vsync := metronome.Variable()
	vsync.MinInterval = time.Second / 240
	vsync.MaxDelta = 250 * time.Millisecond

	supervisor := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](vsync)},
		},
	}
	if _, err := supervisor.Spawn(config, runtime.GOMAXPROCS(0),
		metronome.NewPlugin(),
		platform.NewPlugin(),
		gfx.NewPlugin(),
		gogpu.NewPlugin(), // the backend, windows and input
		render.NewPlugin(),
		&game{supervisor: supervisor, files: assets},
	); err != nil {
		panic(err)
	}
	for supervisor.WaitMainThread() {
		supervisor.TickMainThread()
	}
}
```

`assets/quad.wgsl`, beside `crate.png`:

```wgsl
struct VertexOutput {
	@builtin(position) position: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@group(1) @binding(0) var picture: texture_2d<f32>;
@group(1) @binding(1) var picture_sampler: sampler;

@vertex
fn vertex_main(@location(0) position: vec2<f32>, @location(1) uv: vec2<f32>) -> VertexOutput {
	var out: VertexOutput;
	out.position = vec4<f32>(position, 0.0, 1.0);
	out.uv = uv;
	return out;
}

// encode_srgb encodes linear colour as sRGB: the window's framebuffer is
// BGRA8Unorm, which stores what the last pass writes as it is.
fn encode_srgb(linear: vec3<f32>) -> vec3<f32> {
	let low = linear * 12.92;
	let high = 1.055 * pow(linear, vec3<f32>(1.0 / 2.4)) - 0.055;
	return select(high, low, linear <= vec3<f32>(0.0031308));
}

@fragment
fn fragment_main(in: VertexOutput) -> @location(0) vec4<f32> {
	let colour = textureSample(picture, picture_sampler, in.uv);
	return vec4<f32>(encode_srgb(colour.rgb), colour.a);
}
```

The material names no entry points, so it runs the shader's only vertex and
fragment functions. Its `State` is zero: no blending, no depth test, no
culling. The texture is sampled through its sRGB format, so the shader
reads linear colour and encodes it again for the window.

## The plugin and its Config

render is listed after gfx and platform, and every plugin that draws after
render. Its `Config` sits under `render.PluginName`, and both fields have a
default when zero:

```go
var renderConfig = map[kernel.PluginName]any{
	render.PluginName: &render.Config{
		// Pooled textures, arena chunks, staging buffers and draw-parameter
		// bind groups go after 30 frames unused; zero means 10.
		TemporaryIdleFrames: 30,
		// Each command list takes arena chunks of 4 MiB; zero means 1 MiB.
		TemporaryChunkSize: 4 << 20,
	},
}
```

render installs `Handlers`, its handler of each gfx event it subscribes to,
for a plugin listed after it to order its own handler of that gfx event
around with `configurator.OrderSubscriptions`. That is the application's
business, as the quick start's pump is: a plugin that draws subscribes to
render's events, never to gfx's.

```go
func orderAfterRenderEndsTheFrame(configurator *kernel.Configurator, mine kernel.Sub[gfx.RenderEvent]) {
	handlers, _ := configurator.Resource[render.Handlers]()
	configurator.OrderSubscriptions(handlers.EndFrame, mine)
}
```

## The frame

render publishes `FrameEvent` inside gfx's `DrawEvent` every render beat,
even when no window has a framebuffer. A handler schedules its recording
tasks after `Ready`, the job readying the frame once gfx has acquired its
framebuffers, and joins them into the out dependency it returns, as the quick
start's does, with no ordering among the handlers: gfx flushes and presents
the frame after the last of them. Frames record one at a time: a frame's
`Ready` waits for the previous frame's present.

`FrameEvent` embeds the render track's `metronome.Beat`, and carries
`AcquireWait`, how long the previous frame's acquire blocked, and
`GPULatency`, the time from the newest finished frame's submit to when it
was seen complete, both by metronome's clock and zero until known: under
Fifo, a long acquire wait is headroom and a short one a frame near its
budget, and a growing GPU latency calls for less work.

A task reaches render through three parameters, declared in its signature
as `kernel.Read` is. Each declares gfx's and platform's locks for its user:

| Parameter | Locks | Carries |
|---|---|---|
| `Inventory` | render's asset table, `gfx.Device` and `platform.FS`, all read | every asset call, and `ReloadChanged` |
| `Frame` | render's frame state, its asset table and `gfx.Device`, all read | command lists, temporary textures, capture, readback, inspections and pass timings |
| `Surface` | `gfx.Device`, write | `Request(SurfaceRequest)` |

Tasks holding `Inventory` and `Frame` run together; a task that records and
changes materials declares both, and the kernel merges the duplicate locks.
Each is valid only while its task's job runs: a task never keeps one.

## Loading and sharing assets

A loaded asset is read from a path in platform's files, through every mount
bound to `platform.ReadMountSocket`:

```go
// Crate is a plugin's resource holding the assets it loaded for a crate.
type Crate struct {
	Albedo  render.Texture
	Normals render.Texture
	Shader  render.Shader
	Mesh    render.Buffer
}

func loadCrate(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, crate kernel.Write[Crate]) kernel.NoResult {
	crate.Set(Crate{
		Albedo:  inventory.AcquireTexture("crate/albedo.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB, Mipmaps: true}),
		Normals: inventory.AcquireTexture("crate/normals.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear, Mipmaps: true}),
		Shader:  inventory.AcquireShader("lit.wgsl", render.ShaderDefine{Name: "NORMAL_MAP"}),
		Mesh:    inventory.AcquireBuffer("crate/mesh.bin", render.BufferUsageFlagVertex|render.BufferUsageFlagIndex),
	})
	return kernel.NoResult{}
}

func unloadCrate(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, crate kernel.Read[Crate]) kernel.NoResult {
	inventory.ReleaseTexture(crate.Get().Albedo)
	inventory.ReleaseTexture(crate.Get().Normals)
	inventory.ReleaseShader(crate.Get().Shader)
	inventory.ReleaseBuffer(crate.Get().Mesh)
	return kernel.NoResult{}
}
```

- **Ready in the frame that asks.** The GPU object exists when the call
  returns: a texture is made from its file's header alone, a buffer from the
  file's size rounded up to a multiple of 4, and a shader is compiled in the
  caller's job. render's jobs write texture bodies and buffer bytes before
  gfx submits the frame, whether the asset was acquired before the frame or
  while it records, so a draw recorded right after the acquire reads them.
  One acquired by a task the frame does not wait for lands in the next
  frame's submit instead. There is no loading state, ready event or
  callback.
- **Textures** load from PNG and JPEG, 8 bits per channel. `ColorSpaceSRGB`,
  the zero value, gives an sRGB format, so sampling answers linear colour;
  `ColorSpaceLinear` answers the file's values as they are, for normal maps
  and masks. `Mipmaps` gives every level down to one texel, drawn from the
  level above (see [Mips](#mips)).
- **Buffers** carry the usage their caller declares, and the copy
  destination render fills them through. gfx refuses any other use. Maps are
  not offered.
- **Sharing.** Every acquire of the same asset key shares one asset, counted:
  a texture's key is its path and options, a shader's its path and its
  defines sorted by name, a buffer's its path and usage. Each acquire is
  given back with its release. When the count reaches zero, the asset is
  freed at the end of that frame, after gfx's present, so the frame's draws
  still read it, and an acquire of its key before then holds it again.
- **Failure.** A texture whose file cannot be read or decoded draws magenta:
  one texel when its header fails, its own size when its body does. A buffer
  whose file is missing, a folder or empty gets no bytes, and a shader that
  fails to preprocess or compile keeps no module: each skips every draw that
  uses it. A buffer whose bytes cannot be read when they are written keeps
  zeros. Each failure is reported once per key, with `ReportKindLoadFailed`. A release no acquire
  holds, the zero handle among them, is refused and reported.
- **Handles are transient**, valid only while the program runs: a save
  holds what an asset was loaded from, and a restore acquires it again.

Bytes that are not files, such as a glTF's embedded image, go into a memory
mount, platform's `filesystems.NewMemoryFS()`, which its owner binds as a
read mount and keeps as a resource of its own, and load from there by path:

```go
func initModelLoader(configurator *kernel.Configurator) {
	memory := filesystems.NewMemoryFS()
	configurator.BindSocket(platform.ReadMountSocket{FS: memory, Prefix: "models"})
	configurator.InitResource(memory)
}

func loadEmbeddedImage(k *kernel.Kernel, image []byte, memory kernel.Read[*filesystems.MemoryFS], inventory render.Inventory) render.Texture {
	if err := memory.Get().Put("ship/0.png", image); err != nil {
		k.Logger().LogErr(err)
		return render.Texture{}
	}
	return inventory.AcquireTexture("models/ship/0.png", render.TextureOptions{})
}
```

## Made assets and writes

A made asset is built from memory or from other assets. It has no key, owns
nothing it names, and its maker gives it back once; a second release is
refused and reported.

- `MakeBuffer[T](label, usage, data)` makes a buffer of `data`'s bytes as Go
  lays them out, rounded up to a multiple of 4 with zeros. `T` is plain data
  with no pointers: a number, or an array or struct of them.
- `MakeTexture[T](descriptor, data)` makes a two-dimensional texture, with
  layers, filling mip level 0 layer after layer, each layer's rows packed
  tight; no data leaves it zero. A made texture carries every usage its
  format allows, so it can be a pass target kept across frames, such as a
  history or a render-to-texture layer, sampled, copied, and bound for
  storage where its format allows.
- `MakeMesh(MeshDescriptor)` makes a mesh of vertex buffers, each a
  `BufferRange` with its `VertexLayout`, an optional `IndexBuffer` and a
  `PrimitiveTopology`, whose zero is a triangle list. A draw covers the
  whole mesh: as many vertices as the fewest its vertex ranges hold, or as
  many indices as its index range holds. A part of a mesh is a mesh of its
  own, over ranges of the same buffers. A mesh of misaligned ranges,
  strides or attributes, of more vertex buffers than the device allows, or
  of an unknown topology is refused with `ReportKindMeshRefused`, answering
  the zero `Mesh`.
- `MakeMaterial(MaterialDescriptor)` makes a material; see
  [Materials](#materials).

```go
// heightfield is the terrain's made assets.
type heightfield struct {
	Heights render.Texture
	Grid    render.Buffer
	Patch   render.Mesh
}

func makeHeightfield(inventory render.Inventory, heights []float32, grid [][2]float32) heightfield {
	texture := inventory.MakeTexture(render.TextureDescriptor{
		Label: "heights", Width: 256, Height: 256, Format: render.TextureFormatR32Float,
	}, heights)
	buffer := inventory.MakeBuffer("grid", render.BufferUsageFlagVertex, grid)
	patch := inventory.MakeMesh(render.MeshDescriptor{
		Label: "patch",
		VertexBuffers: []render.VertexBuffer{{
			Range:  render.BufferRange{Buffer: buffer},
			Layout: render.VertexLayout{Stride: 8, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x2}}},
		}},
		Topology: render.PrimitiveTopologyTriangleStrip,
	})
	return heightfield{Heights: texture, Grid: buffer, Patch: patch}
}

func releaseHeightfield(inventory render.Inventory, field heightfield) {
	inventory.ReleaseMesh(field.Patch)
	inventory.ReleaseBuffer(field.Grid)
	inventory.ReleaseTexture(field.Heights)
}
```

**Writes** fill a buffer or a texture region from a slice, landing before
every command list of gfx's next submit, so a write fills the asset for the
whole frame being recorded, or the next one once that frame is flushed; two
writes to one region in a frame leave the last. `WriteBuffer` writes at an
offset and of a length that are multiples of 4; `WriteTexture` writes a
`TextureRegion` of one mip level and one layer, a zero width or height
meaning the rest of the level, so an atlas page lands in a layer of a
2D-array texture. A write that does not fit is refused with
`ReportKindWriteRefused`. To change data between two passes, take a
temporary buffer and copy it at that point of a command list (see
[Copies](#copies-and-buffer-clears)).

```go
// light mirrors struct Light { position: vec3<f32>, radius: f32 }.
type light struct {
	Position [3]float32
	Radius   float32
}

// Lighting is a plugin's resource: its light buffer, made with room for
// every light, and a glyph atlas of RGBA8 layers.
type Lighting struct {
	LightBuffer render.Buffer
	Lights      []light
	Atlas       render.Texture
	Glyph       []byte
}

func writeLightsAndGlyph(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, lighting kernel.Read[Lighting]) kernel.NoResult {
	inventory.WriteBuffer(lighting.Get().LightBuffer, 0, lighting.Get().Lights)
	// A 16×16 glyph into layer 2 of the atlas.
	inventory.WriteTexture(lighting.Get().Atlas, render.TextureRegion{Layer: 2, X: 64, Y: 32, Width: 16, Height: 16}, lighting.Get().Glyph)
	return kernel.NoResult{}
}
```

**Checking a layout.** `CheckLayout[T](shader, binding, path)` answers why
a Go type cannot be the type at `path` within one of a shader's bindings,
by the rules a uniform block's value is checked by: its size, and each
field's offset against the type's members in order, names ignored and `_`
fields left out as padding. The path walks dotted members, `[]` stepping
into an array's element: `[]` is an element of a storage buffer of
`array<Light>`, `lights[]` an element of a uniform block's member `lights`,
`[].argument` the member `argument` of an element, and an empty path the
binding's whole type. A `[]` compares the element's type, not its array's
stride: a slice of a struct fills the array, but one of `[3]float32` does
not fill an `array<vec3<f32>>`, whose elements lie 16 bytes apart. A shader
that failed, a binding it lacks or of no buffer, and a path it cannot walk
answer an error too. Nothing is reported
or kept, so check each type once for each shader, and report what you are
answered.

```go
// checkLights runs once, after the lit shader is acquired.
func checkLights(k *kernel.Kernel, shader render.Shader, inventory render.Inventory) kernel.NoResult {
	// The lit shader's storage buffer lights, an array<Light>.
	if err := inventory.CheckLayout[light](shader, "lights", "[]"); err != nil {
		k.Logger().LogErr(err)
	}
	return kernel.NoResult{}
}
```

## Shaders and variants

Shaders are WGSL, preprocessed by gfx's preprocessor with `#include`,
`#define`, `#if` and `#const`. A **variant** is a shader acquired with its
defines: a `ShaderDefine` with no value is a flag read by `#if`; one with a
value overrides the `#const` of its name, as WGSL text. Defines are sorted by
name, so two acquires listing them in another order share one variant.

```go
func acquireSkinned(inventory render.Inventory) render.Shader {
	return inventory.AcquireShader("lit.wgsl",
		render.ShaderDefine{Name: "SKINNED"},
		render.ShaderDefine{Name: "JOINTS", Value: "4u"},
	)
}
```

Compiling is slow, and it happens in the caller's job: a variant acquired
first while a frame records stalls that recording, so a game acquires its
shaders behind a loading screen. A second acquire of a held variant, of up
to 8 defines joined in up to 128 bytes, allocates nothing.

A parameter's level is its group, and every shader follows the same
layout:

| Group | Holds | Set with |
|---|---|---|
| `@group(0)` | pass parameters | a pass's `SetPass…` |
| `@group(1)` | material parameters | `Inventory.SetMaterial…` |
| `@group(2)` | draw parameters | a pass's `Set…` |
| `@group(3)` | reserved: a shader declaring it is refused | |

render builds every layout itself, sharing identical ones, so shaders
sharing an include share its `@group(0)` layout. A shader may declare at
most 8 uniform blocks in `@group(0)` and `@group(2)`, 4 storage buffers in
`@group(2)`, and 8 storage buffers a stage sees. A shader past a limit, or
declaring `@group(3)`, is refused with `ReportKindShaderRefused`, and one
that fails to preprocess or compile is reported with `ReportKindLoadFailed`;
either draws nothing.

**Entry points** follow WebGPU's rule. A material names its vertex and
fragment functions, and a compute pass its compute function; an empty name
means the shader's only function of that stage. A file with several, such
as a depth-only path beside a colour path, names the one it wants:

```go
func makeDepthOnly(inventory render.Inventory, lit render.Shader) render.Material {
	return inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "crate depth",
		Shader:           lit,
		VertexEntryPoint: "vertex_depth",
		State:            render.State{Depth: render.DepthState{Compare: render.CompareFunctionLess, Write: true}},
	})
}
```

Entry points are not part of a variant's key: one module serves every
function in it.

## Materials

A material is a shader variant with its render state, its entry points, and
the values bound to its `@group(1)` parameters by name. render ships none:
each plugin makes its own.

```go
func makeGlass(inventory render.Inventory, shader render.Shader, albedo render.Texture) render.Material {
	state := render.State{
		Depth:    render.DepthState{Compare: render.CompareFunctionLessEqual},
		CullMode: render.CullModeBack,
	}
	state.ColorTargets[0].Blend = render.BlendState{
		Color: render.BlendComponent{SourceFactor: render.BlendFactorSourceAlpha, DestinationFactor: render.BlendFactorOneMinusSourceAlpha},
		Alpha: render.BlendComponent{DestinationFactor: render.BlendFactorOneMinusSourceAlpha},
	}
	glass := inventory.MakeMaterial(render.MaterialDescriptor{Label: "glass", Shader: shader, State: state})
	inventory.SetMaterialTexture(glass, "albedo", albedo)
	inventory.SetMaterialSampler(glass, "albedo_sampler", render.SamplerDescriptor{
		AddressModeU: render.AddressModeRepeat, AddressModeV: render.AddressModeRepeat,
		MagFilter: render.FilterModeLinear, MinFilter: render.FilterModeLinear, MipmapFilter: render.FilterModeLinear,
		MaxAnisotropy: 8,
	})
	inventory.SetMaterialUniform(glass, "surface", surfaceBlock{Tint: [4]float32{0.8, 0.9, 1, 0.4}, Roughness: 0.1})
	return glass
}

// surfaceBlock mirrors lit.wgsl's struct Surface { tint: vec4<f32>, roughness: f32 }:
// WGSL rounds the struct's size up to 32, its alignment of 16, so the Go
// struct pads it.
type surfaceBlock struct {
	Tint      [4]float32
	Roughness float32
	_         [3]float32
}
```

**Render state** is one comparable `State`, whose zero value, and the zero
value of each field, is WebGPU's default: no blending, every channel
written, the depth test passing always with nothing written, the stencil
test passing always, no culling, counter-clockwise faces in front. Its
fields are phrased so zero means the default: a mask names the channels or
bits left out, `UnwrittenChannels` and `UnreadBits`, not those kept.

**Values by name.** `SetMaterialTexture`, `SetMaterialTextureView`,
`SetMaterialSampler`, `SetMaterialBuffer` and `SetMaterialUniform[T]` each
bind one whole binding, checked when set:

- A name the shader lacks is ignored, so one set of values serves every
  variant; the name of a binding outside `@group(1)` is refused.
- A uniform block's Go type is checked once per shader and binding: its
  size, and each field's offset against the block's members in order, names
  ignored and `_` fields left out as padding. A type that is no struct, such
  as `[4]float32`, fills a block that is no struct of its size.
- A value that cannot fill its binding (another kind, a struct of another
  layout, a view of another dimension or sample type, mips or layers its
  texture lacks, a buffer too small or made without the binding's usage, an
  offset no multiple of 256) is refused, reported once per shader and name
  with `ReportKindParameterRefused`, and the binding draws its default.
- A texture or buffer freed while a material binds it unbinds at the end of
  that frame. A material owns nothing it names, so a draw of it after its
  shader is freed is skipped.

**Samplers** are given by value, and equal ones share one gfx sampler. A
`SamplerDescriptor`'s zero value is WebGPU's default sampler: clamping to the
edge, nearest filtering, every mip level. A comparison sampler, whose
`Compare` is set, fills only a comparison binding.

**Texture views** bind part of a texture, or the texture seen another way,
wherever a texture binds: `TextureView{Texture, BaseMipLevel, MipLevelCount,
BaseLayer, LayerCount, Dimension, Aspect}`, zero counts meaning the rest and a
zero dimension or aspect the binding's. A texture handle alone is the whole
texture; a storage binding given a whole texture sees its first mip level.

```go
func bindSky(inventory render.Inventory, sky render.Material, cube, shadows render.Texture, stars render.Buffer) {
	// A texture of 6 layers seen as a cube.
	inventory.SetMaterialTextureView(sky, "environment", render.TextureView{Texture: cube, Dimension: render.TextureViewDimensionCube})
	// One layer of a shadow-map array, seen as 2D.
	inventory.SetMaterialTextureView(sky, "sun_shadow", render.TextureView{Texture: shadows, BaseLayer: 2, Dimension: render.TextureViewDimension2D})
	// A storage buffer of the stars, whole.
	inventory.SetMaterialBuffer(sky, "stars", render.BufferRange{Buffer: stars})
}
```

**Unbound defaults.** A binding nothing fills draws a fixed default, so an
optional texture needs no placeholder:

| Binding | Draws with |
|---|---|
| float texture | 1×1 white, in every view dimension |
| depth texture | 1×1 at depth 1.0 |
| integer texture | zeros |
| sampler | linear, clamping to the edge |
| comparison sampler | less-equal |
| uniform block | zeros of its size |
| multisampled texture | zeros |
| storage buffer or texture | a placeholder when no running function reads it; otherwise the draw is skipped, reported once |

**When a change shows.** A draw uses the values its material holds when the
draw is recorded. A change after the material's first draw in the frame
makes a **version**, a bind group for the draws after it, which allocates;
values that change between most draws belong in draw parameters instead. A change made by
one task while another records draws of the material may not show in those
draws: it shows for certain in the draws the task that made it records
after it, and in those recorded after a task the kernel orders before the
frame's recording.

```go
func recordFadingGlass(inventory render.Inventory, pass *render.Pass, glass render.Material, pane render.Mesh) {
	pass.SetMaterial(glass)
	// Not drawn yet this frame: the material's own bind group takes it.
	inventory.SetMaterialUniform(glass, "surface", surfaceBlock{Tint: [4]float32{0.8, 0.9, 1, 0.4}})
	pass.Draw(pane, render.DrawArgs{})
	// Drawn already this frame: the draws after it bind a version.
	inventory.SetMaterialUniform(glass, "surface", surfaceBlock{Tint: [4]float32{0.8, 0.9, 1, 0.2}})
	pass.Draw(pane, render.DrawArgs{})
}
```

## Command lists

A frame is command lists, not a graph. A command list is one task's run of
passes, compute passes, copies and readbacks, kept in the order recorded,
from `Frame.BeginCommandList(label, order)` to its `End`. Lists fill
concurrently, each from one task, and the frame runs them by their
**order**, lowest first. The orders are one space every user of render
shares, so an application names them in one place, with gaps:

```go
const (
	orderParticles render.Order = 50
	orderShadows   render.Order = 100
	orderOpaque    render.Order = 200
	orderOpaque2   render.Order = 201
	orderUI        render.Order = 900
	orderDebug     render.Order = 1000
)
```

- Two lists of a frame at one order are refused with
  `ReportKindOrderTaken`, and a list at `math.MinInt32`, render's mip list,
  or `math.MaxInt32`, its closing list, with `ReportKindOrderReserved`.
- A list begun while no frame records, such as from a task the frame does
  not wait for, is refused with `ReportKindListOutsideFrame`.
- A refused list records into nothing: its calls are harmless, and its
  passes record nothing.
- `End` hands the list's arena chunks to gfx, one write each. A list never
  ended uploads none of its temporary buffers or uniform blocks.

A heavy pass is recorded in parallel as split passes: consecutive passes
into the same targets in lists at consecutive orders, each after the first
keeping what its targets hold.

```go
func recordOpaqueHalf(k *kernel.Kernel, half int, frame render.Frame, scene kernel.Read[Scene]) kernel.NoResult {
	target := render.ColorTarget{Window: scene.Get().Window}
	order := orderOpaque
	if half == 1 {
		order = orderOpaque2
	} else {
		target.Clear = true // only the first half clears
	}
	list := frame.BeginCommandList("opaque", order)
	pass := list.BeginPass(render.PassDescriptor{Label: "opaque", ColorTargets: []render.ColorTarget{target}})
	// ... this half's draws
	pass.End()
	list.End()
	return kernel.NoResult{}
}
```

## Passes

`CommandList.BeginPass(PassDescriptor)` begins a pass into up to 8 colour
targets and a depth-stencil target, each a window or a texture at one mip
level and one layer or cube face:

- A **colour target** names a window or a texture of a colour format. It
  keeps what the target holds unless `Clear` clears it to `ClearColor`, and
  stores what the pass draws unless `Discard` throws it away. A
  multisampled target resolves into its `Resolve`, a window or a texture of
  one sample.
- The **depth-stencil target** is a texture of a depth or stencil format,
  cleared to `ClearDepth` and `ClearStencil`, or kept. A `ReadOnly` one is
  tested against without being written, so the pass may sample it too; it
  is neither cleared nor discarded.
- The sample count and formats follow from the targets, which share one
  size and sample count.

```go
func recordLitPass(list *render.CommandList, window platform.Window, colour, depth render.Texture) render.Pass {
	return list.BeginPass(render.PassDescriptor{
		Label: "lit",
		ColorTargets: []render.ColorTarget{{
			Texture: colour, // 4 samples
			Clear:   true,
			Discard: true, // only the resolved picture is kept
			Resolve: render.ResolveTarget{Window: window},
		}},
		DepthStencil: render.DepthStencilTarget{Texture: depth, Clear: true, ClearDepth: 1, Discard: true},
	})
}
```

A **window** is drawn into, never sampled or copied from, with no implicit
blit: what reaches it is what the last pass into it draws. Its framebuffer is
`BGRA8Unorm` on gogpu's surfaces, so the last pass encodes sRGB in its
shader, as the quick start's does. A pass into a window with no framebuffer
this frame, minimized or hidden, even as a resolve target, is skipped
silently, and so are its draws.

A pass whose targets disagree or that has none is refused with
`ReportKindPassTargetsRefused`, and its draws skipped; `BeginPass`'s doc
comment lists each case.

**Clearing what another pass drew.** After the users' recording, render
reports each pass that clears a target, colour or depth-stencil, an earlier
pass of the frame drew into in submit order, with
`ReportKindClearAfterDraw`, once for each pair, naming both passes' labels
and their lists' orders. A pass that clears depth to draw over what another
drew, as a weapon over the scene, clears a depth-stencil target of its own.

## Draws, instances and pass state

In a pass, `SetMaterial` selects the material of the draws after it, and
`Draw(mesh, DrawArgs{InstanceCount, FirstInstance})` draws a mesh whole, a
zero instance count drawing one. `DrawVertices(topology, vertexCount,
DrawArgs)` draws with no mesh, for full-screen triangles, UI quads and debug
lines, its shader placing each vertex from `vertex_index`:

```go
func recordPost(list *render.CommandList, window platform.Window, tonemap render.Material, scene render.Texture) {
	pass := list.BeginPass(render.PassDescriptor{Label: "tonemap", ColorTargets: []render.ColorTarget{{Window: window}}})
	pass.SetMaterial(tonemap)
	pass.SetTexture("scene", scene)
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
	pass.End()
}
```

`DrawVertices` binds no per-vertex buffer, so a shader whose vertex stage
reads an input no instance buffer feeds fails its pipeline.

**Pipelines.** A draw draws with a pipeline of its material's shader,
entry points and render state, its mesh's vertex layouts and topology, its
instance buffers' layouts and the pass's formats and sample count, shared by
every draw of the same. The first draw of a new combination makes its
pipeline synchronously in the recording job, stalling that recording: a game
warms its pipelines by drawing each early, behind a loading screen. A
pipeline that cannot be made is reported once, with
`ReportKindPipelineFailed`, and its draws skipped until its shader is freed.

**Skipped draws.** A draw naming what cannot be drawn, such as a freed mesh,
a failed shader or no material, is skipped and reported once, with
`ReportKindDrawSkipped`; `Draw`'s doc comment lists each case. Nothing
panics.

**Instances.** A draw's instances read per-instance data two ways:

- a storage buffer read at `instance_index`, a draw's `FirstInstance`
  selecting a batch's rows;
- **instance buffers**, vertex buffers stepped per instance, set with
  `SetInstanceBuffer(slot, InstanceBuffer{Range, Layout})` at slots 0 to 7.
  A draw binds those set after its mesh's vertex buffers, in slot order, so
  its shader reads them by `@location` alone, and their layouts join the
  pipeline's key. One at a slot past 7 or misaligned is refused with
  `ReportKindInstanceBufferRefused`.

```go
// offsetLayout is each tree's offset, read at @location(3). It is made once:
// a layout literal written in the recording task would allocate each frame.
var offsetLayout = render.VertexLayout{Stride: 8, Attributes: []render.VertexAttribute{
	{Format: render.VertexFormatFloat32x2, Location: 3},
}}

func recordTrees(list *render.CommandList, pass *render.Pass, tree render.Mesh, bark render.Material, offsets [][2]float32) {
	pass.SetMaterial(bark)
	pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: list.AcquireTemporaryBuffer(offsets), Layout: offsetLayout})
	pass.Draw(tree, render.DrawArgs{InstanceCount: uint32(len(offsets))})
}
```

**Pass state.** A pass begins with the viewport and scissor rectangle of its
whole targets, a zero stencil reference and a transparent black blend
constant. `SetViewport`, `SetScissorRectangle`, `SetStencilReference` and
`SetBlendConstant` change them for the draws after them, until set again or
the pass ends. A scissor rectangle is clamped to the targets, so a UI's clip
rectangle reaching past a window draws its part inside. A viewport WebGPU
refuses is refused with `ReportKindPassStateRefused`, keeping the one before.

```go
func recordSplitScreen(pass *render.Pass, world render.Mesh, width, height float32) {
	pass.SetViewport(render.Viewport{Width: width / 2, Height: height, MaxDepth: 1})
	pass.Draw(world, render.DrawArgs{})
	pass.SetViewport(render.Viewport{X: width / 2, Width: width / 2, Height: height, MaxDepth: 1})
	pass.SetScissorRectangle(render.ScissorRectangle{X: uint32(width / 2), Width: uint32(width / 2), Height: uint32(height)})
	pass.SetStencilReference(1)
	pass.SetBlendConstant(render.Color{A: 0.5})
	pass.Draw(world, render.DrawArgs{})
}
```

**Draw order.** A pass remembers what its draws found of the last four
materials it drew and of its last mesh. Switching back to one of those
materials costs little; switching to any other repeats the lookup of its
shader, pipeline and bind groups, and alternating meshes repeats the mesh's.
A plugin drawing many materials sorts its draws by shader, then material,
then mesh.

## Pass and draw parameters

A pass's `SetPassTexture`, `SetPassTextureView`, `SetPassSampler`,
`SetPassBuffer` and `SetPassUniform[T]` bind its **pass parameters**, of
`@group(0)`, read by every draw after them in the pass, whatever its
material: a camera's matrices are set once. Its `SetTexture`,
`SetTextureView`, `SetSampler`, `SetBuffer` and `SetUniform[T]` bind **draw
parameters**, of `@group(2)`, read by the draws after them until set again,
until `SetMaterial` or until the pass ends.

```go
// cameraBlock mirrors struct Camera { view_projection: mat4x4<f32> }.
type cameraBlock struct {
	ViewProjection [16]float32
}

// objectBlock mirrors struct Object { model: mat4x4<f32>, tint: vec4<f32> }.
type objectBlock struct {
	Model [16]float32
	Tint  [4]float32
}

// recordObjects draws each object with a draw of its own, its block a
// draw parameter.
func recordObjects(pass *render.Pass, camera cameraBlock, crate render.Mesh, material render.Material, objects []objectBlock) {
	pass.SetPassUniform("camera", camera)
	pass.SetMaterial(material)
	for _, object := range objects {
		pass.SetUniform("object", object)
		pass.Draw(crate, render.DrawArgs{})
	}
}

// recordObjectsInstanced draws the same objects in one draw, its shader
// reading each object's row of a storage buffer at instance_index.
func recordObjectsInstanced(list *render.CommandList, pass *render.Pass, camera cameraBlock, crate render.Mesh, material render.Material, objects []objectBlock) {
	pass.SetPassUniform("camera", camera)
	pass.SetMaterial(material)
	pass.SetBuffer("objects", list.AcquireTemporaryBuffer(objects))
	pass.Draw(crate, render.DrawArgs{InstanceCount: uint32(len(objects))})
}
```

Pass and draw parameters are checked at the next draw against the binding
of their name in the draw's shader, refused and caught as a material's
values are, and a name a shader lacks is ignored. A sampled texture
overlapping a target of the pass it is bound in is caught, reported once,
and draws its default, so the list's encoder survives a bloom chain without
views.

Their uniform blocks go into the list's temporary arena and bind at dynamic
offsets, so draws setting them rebind one bind group at new offsets and make
none; storage buffers given as draw parameters do too, ranges of one buffer
and one size sharing a bind group. A storage buffer given as a pass
parameter binds statically: a made or loaded buffer costs nothing from frame
to frame, but a temporary range costs the pass a new bind group in each
frame it moves. Data a whole pass reads goes in a made buffer written with
`WriteBuffer` each frame.

## Compute passes

`CommandList.BeginComputePass(label)` begins a run of dispatches among the
list's passes. There is no compute material: `SetShader(shader, entryPoint)`
selects a compute function, an empty name the shader's only one, and resets
every parameter; `SetTexture`, `SetTextureView`, `SetSampler`, `SetBuffer`
and `SetUniform[T]` bind parameters by name in any group; and
`Dispatch(x, y, z)` runs it.

```go
// stepBlock mirrors struct Step { delta: f32, count: u32 }.
type stepBlock struct {
	Delta float32
	Count uint32
}

// Particles is a plugin's resource: its simulation shader and the storage
// buffer of its particles.
type Particles struct {
	Simulate render.Shader
	Buffer   render.Buffer
	Count    uint32
}

func recordParticles(k *kernel.Kernel, event render.FrameEvent, frame render.Frame, particles kernel.Read[Particles]) kernel.NoResult {
	const workgroupSize = 64
	count := particles.Get().Count
	list := frame.BeginCommandList("particles", orderParticles)
	compute := list.BeginComputePass("simulate")
	compute.SetShader(particles.Get().Simulate, "")
	compute.SetBuffer("particles", render.BufferRange{Buffer: particles.Get().Buffer})
	compute.SetUniform("step", stepBlock{Delta: float32(event.Delta.Seconds()), Count: count})
	compute.Dispatch((count+workgroupSize-1)/workgroupSize, 1, 1)
	compute.End()
	list.End()
	return kernel.NoResult{}
}
```

Each group binds as it does for draws (see
[Pass and draw parameters](#pass-and-draw-parameters)), so a temporary range
in `@group(1)`, which binds statically, costs a bind group each frame it
moves. A dispatch is skipped and reported once, with
`ReportKindDispatchSkipped`, for no shader selected, one that failed or was
freed, or a storage binding its function reads left unbound. A pipeline's
first dispatch makes it, as a draw's does; a missing or ambiguous entry
point is that pipeline's failure.

## Copies and buffer clears

A list's `CopyBuffer`, `CopyTexture`, `CopyBufferToTexture`,
`CopyTextureToBuffer` and `ClearBuffer` record at their point among its
passes. A range's zero size and a region's zero width or height mean the
rest. Between a buffer and a texture, each row of the region starts a
multiple of 256 bytes after the one before, which WebGPU asks of a copy:
the caller sizes the range for it. A copy that does not fit, of a buffer
made without `BufferUsageFlagCopySrc` among others, is refused with
`ReportKindCopyRefused`, recording nothing.

```go
func recordCopies(list *render.CommandList, staging, vertices render.Buffer, history, colour render.Texture) {
	// Data that changes between two passes: a temporary buffer, copied in at
	// this point of the list.
	moved := list.AcquireTemporaryBuffer([]float32{0, 1, 2, 3})
	list.CopyBuffer(moved, render.BufferRange{Buffer: vertices, Offset: 256})
	// This frame's picture, kept for the next frame as its history.
	list.CopyTexture(colour, render.TextureRegion{}, history, render.TextureRegion{})
	// A 64×64 RGBA8 region from a buffer into a texture's corner, and back:
	// rows of 64 texels of 4 bytes, each 256 bytes after the one before.
	list.CopyBufferToTexture(render.BufferRange{Buffer: staging}, history, render.TextureRegion{Width: 64, Height: 64})
	list.CopyTextureToBuffer(history, render.TextureRegion{Width: 64, Height: 64}, render.BufferRange{Buffer: staging})
	// The first 256 bytes zeroed.
	list.ClearBuffer(render.BufferRange{Buffer: vertices, Size: 256})
}
```

## Temporaries

Temporaries live for one frame, and their handles name them only until it
ends: a later frame may hand the same texture, bytes or mesh entry to
another user under the same handle, so one kept past its frame silently
reads or draws into theirs.

- **Temporary textures**, `Frame.AcquireTemporaryTexture(descriptor)`, come
  from a pool by descriptor, its label included, with every usage the format
  allows, held until the frame ends with no reuse within it. Their contents
  are undefined when taken. A user hands one between its tasks through state
  of its own, acquired in a task the kernel orders before those that draw
  into or sample it.
- **Temporary buffers**, `CommandList.AcquireTemporaryBuffer[T](data)`, are
  256-byte-aligned ranges of the list's arena chunks holding `data`: vertex
  or index slices, rows for a storage parameter, bytes for a copy. Taking
  one makes no gfx object. Its bytes reach the GPU at the list's `End`.
- **Temporary meshes**, `CommandList.AcquireTemporaryMesh(MeshDescriptor)`,
  are meshes over buffer ranges valid as long, drawn with `Draw` as any
  mesh is. Each command of a Dear ImGui-style UI is one, over sub-ranges of
  the frame's vertices and indices: the index range's offset is its first
  index and the vertex range's its base vertex.

```go
// Post is a plugin's resource: the material its tonemapping pass draws with.
type Post struct {
	Tonemap render.Material
}

// recordHDR draws the scene into a temporary HDR texture and tonemaps it
// into the window, in one list, so the texture needs handing to no other
// task.
func recordHDR(k *kernel.Kernel, event render.FrameEvent, frame render.Frame, scene kernel.Read[Scene], post kernel.Read[Post]) kernel.NoResult {
	hdr := frame.AcquireTemporaryTexture(render.TextureDescriptor{
		Label: "hdr", Width: 1280, Height: 720, Format: render.TextureFormatRGBA16Float,
	})
	list := frame.BeginCommandList("hdr", orderOpaque)
	pass := list.BeginPass(render.PassDescriptor{Label: "hdr scene", ColorTargets: []render.ColorTarget{{Texture: hdr, Clear: true}}})
	pass.SetMaterial(scene.Get().Material)
	pass.Draw(scene.Get().Quad, render.DrawArgs{})
	pass.End()
	recordPost(&list, scene.Get().Window, post.Get().Tonemap, hdr)
	list.End()
	return kernel.NoResult{}
}
```

```go
// uiVertex mirrors the UI shader's vertex input.
type uiVertex struct {
	Position [2]float32
	UV       [2]float32
	Colour   uint32
}

var uiLayout = render.VertexLayout{Stride: 20, Attributes: []render.VertexAttribute{
	{Format: render.VertexFormatFloat32x2, Offset: 0, Location: 0},
	{Format: render.VertexFormatFloat32x2, Offset: 8, Location: 1},
	{Format: render.VertexFormatUnorm8x4, Offset: 16, Location: 2},
}}

// uiCommand is one clipped run of the UI's indices.
type uiCommand struct {
	Clip       render.ScissorRectangle
	FirstIndex uint32
	IndexCount uint32
}

// UI is the frame a UI library built: its window, material, vertices,
// 16-bit indices and commands.
type UI struct {
	Window   platform.Window
	Material render.Material
	Vertices []uiVertex
	Indices  []uint16
	Commands []uiCommand
}

func recordUI(k *kernel.Kernel, event render.FrameEvent, frame render.Frame, ui kernel.Read[UI]) kernel.NoResult {
	list := frame.BeginCommandList("ui", orderUI)
	vertices := list.AcquireTemporaryBuffer(ui.Get().Vertices)
	indices := list.AcquireTemporaryBuffer(ui.Get().Indices)
	pass := list.BeginPass(render.PassDescriptor{Label: "ui", ColorTargets: []render.ColorTarget{{Window: ui.Get().Window}}})
	pass.SetMaterial(ui.Get().Material)
	for _, command := range ui.Get().Commands {
		mesh := list.AcquireTemporaryMesh(render.MeshDescriptor{
			Label:         "ui",
			VertexBuffers: []render.VertexBuffer{{Range: vertices, Layout: uiLayout}},
			Indices: render.IndexBuffer{
				Range:  render.BufferRange{Buffer: indices.Buffer, Offset: indices.Offset + 2*uint64(command.FirstIndex), Size: 2 * uint64(command.IndexCount)},
				Format: render.IndexFormatUint16,
			},
		})
		pass.SetScissorRectangle(command.Clip)
		pass.Draw(mesh, render.DrawArgs{})
	}
	pass.End()
	list.End()
	return kernel.NoResult{}
}
```

## Mips

A texture acquired with `TextureOptions{Mipmaps: true}` gets every level
below the first drawn, layer by layer, each from the level above through a
linear filter, an sRGB texture through sRGB views so its levels are filtered
in linear colour. They are drawn in render's own command list at
`math.MinInt32`, which gfx submits before every user's, in the frame the
texture's body is written, so a draw recorded after the acquire samples
them, and drawn again after a reload or a device recovery.

The mip shader is one of render's built-in assets, `cog.render/mipshader.wgsl`,
mounted under `cog.render` at the lowest priority: a mount holding a file of
that name overrides it. A made texture's levels below the first stay zero
until written; a plugin draws them itself, as a bloom chain does, each level
a pass sampling a view of the level above:

```go
func recordDownsample(list *render.CommandList, bloom render.Texture, downsample render.Material, levelCount uint32) {
	for level := uint32(1); level < levelCount; level++ {
		pass := list.BeginPass(render.PassDescriptor{
			Label:        "bloom downsample",
			ColorTargets: []render.ColorTarget{{Texture: bloom, MipLevel: level}},
		})
		pass.SetMaterial(downsample)
		pass.SetTextureView("source", render.TextureView{Texture: bloom, BaseMipLevel: level - 1, MipLevelCount: 1})
		pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
		pass.End()
	}
}
```

## Capture

`Frame.RequestCapture(window)`, called at any time, asks for the window's
picture as the first frame acquired after the call draws it. render
publishes `CapturedEvent{Window, Beat, Status}` a frame later, and
`Frame.ReadCapture(event)` answers the picture, as `*image.NRGBA` in RGBA
order with alpha opaque and no colour conversion, only while that
publication's jobs run: its handler reads it in a task joined into its out
dependency, and keeps a copy.

```go
// Screenshots is a plugin's resource: the PNGs captured so far.
type Screenshots [][]byte

func requestScreenshot(k *kernel.Kernel, window platform.Window, frame render.Frame) kernel.NoResult {
	if err := frame.RequestCapture(window); err != nil {
		k.Logger().LogErr(err)
	}
	return kernel.NoResult{}
}

func subscribeScreenshots(configurator *kernel.Configurator) {
	save := configurator.RegisterTask2(saveScreenshot)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.CapturedEvent) (render.CapturedEvent, kernel.JobHandle) {
		return event, ex.Schedule(save, event, in).Handle()
	})
}

func saveScreenshot(k *kernel.Kernel, event render.CapturedEvent, frame render.Frame, shots kernel.Write[Screenshots]) kernel.NoResult {
	if event.Status != render.CaptureStatusCaptured {
		return kernel.NoResult{} // not acquired that frame, or failed and logged
	}
	picture, err := frame.ReadCapture(event)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	shots.Set(append(shots.Get(), encoded.Bytes()))
	return kernel.NoResult{}
}
```

A window already being captured answers `ErrCaptureBusy` until its event is
published; one with no surface yet, or not 8-bit BGRA or RGBA,
`ErrCaptureUnsupported`; any window `ErrDeviceLost` while the device is
lost.

## Readback

A list's `ReadBuffer(range)` and `ReadTexture(texture, region)` read bytes
back as they are at that point of the list: each records a copy into a
staging buffer from render's pool and answers a `Readback`. Once the frame
has run on the GPU, a frame or more later, render publishes
`ReadbackEvent{Readback, Status}`, and `Frame.ReadbackData(event)` answers
the bytes while that publication's jobs run, a texture's rows 256 bytes
apart. After that the staging buffer goes back to the pool, so a task copies
what it keeps.

```go
// Picking is a plugin's resource: a texture of object IDs the scene draws,
// the texel under the pointer, the readback in flight and the ID read.
type Picking struct {
	IDs     render.Texture
	X       uint32
	Y       uint32
	Pending render.Readback
	Picked  uint32
}

func recordPick(k *kernel.Kernel, event render.FrameEvent, frame render.Frame, picking kernel.Write[Picking]) kernel.NoResult {
	pick := picking.Get()
	if pick.Pending != (render.Readback{}) {
		return kernel.NoResult{} // one pick in flight at a time
	}
	list := frame.BeginCommandList("pick", orderDebug)
	pick.Pending = list.ReadTexture(pick.IDs, render.TextureRegion{X: pick.X, Y: pick.Y, Width: 1, Height: 1})
	picking.Set(pick)
	list.End()
	return kernel.NoResult{}
}

func readPick(k *kernel.Kernel, event render.ReadbackEvent, frame render.Frame, picking kernel.Write[Picking]) kernel.NoResult {
	pick := picking.Get()
	if event.Readback != pick.Pending {
		return kernel.NoResult{}
	}
	pick.Pending = render.Readback{}
	if data, err := frame.ReadbackData(event); err == nil {
		pick.Picked = binary.LittleEndian.Uint32(data)
	} // a failed readback was logged; the next frame picks again
	picking.Set(pick)
	return kernel.NoResult{}
}
```

`readPick` is scheduled from a `ReadbackEvent` handler after its `in` and
joined into its out dependency, as `saveScreenshot` is from
`CapturedEvent`'s. A readback that does not fit is refused as a copy is,
answering the zero `Readback`.

## The surface request

The surface every window starts with is gfx's `Config.Surface`, set where
the application lists gfx. A task holding the `Surface` parameter changes it
at run time, for every window, the ones opened later included, configured
before the next frame acquires:

```go
func toggleVsync(k *kernel.Kernel, vsync bool, surface render.Surface) kernel.NoResult {
	mode := render.PresentModeImmediate
	if vsync {
		mode = render.PresentModeFifo
	}
	surface.Request(render.SurfaceRequest{PresentMode: mode})
	return kernel.NoResult{}
}
```

`Surface` locks gfx's `Device` for write, so its job waits for every job
holding `Inventory` or `Frame`: it is for a rare toggle. A part a surface
cannot honour falls back to `BGRA8Unorm` and `Fifo`, logged; gogpu's surfaces
offer those alone today.

## Device recovery

When gfx recovers a lost device, render writes every loaded texture and
buffer again from its file under the same handle, requests the mips of those
loaded with `Mipmaps` for the next frame, refills its default textures and
every material's uniform blocks, and then publishes `RecoveredEvent`. Every
made buffer and texture is back under its handle holding nothing: its maker
fills it again from a task its handler schedules. Shaders, pipelines and
bind groups come back by themselves.

```go
// Terrain is a plugin's resource: the heightfield it made, and the slices
// it made it from.
type Terrain struct {
	Field   heightfield
	Heights []float32
	Grid    [][2]float32
}

func subscribeRefill(configurator *kernel.Configurator) {
	refill := configurator.RegisterTask2(refillHeightfield)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.RecoveredEvent) (render.RecoveredEvent, kernel.JobHandle) {
		return event, ex.Schedule(refill, kernel.NoArg{}, in).Handle()
	})
}

func refillHeightfield(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, terrain kernel.Read[Terrain]) kernel.NoResult {
	inventory.WriteTexture(terrain.Get().Field.Heights, render.TextureRegion{}, terrain.Get().Heights)
	inventory.WriteBuffer(terrain.Get().Field.Grid, 0, terrain.Get().Grid)
	return kernel.NoResult{}
}
```

A loaded asset whose file is gone, such as memory-mount bytes since removed,
is reported as a failed load. While the device is lost, every call of
render's answering an error answers `render.ErrDeviceLost`.

## Inspections

An **inspection** is one frame written down in render's words, for an agent
or a tool asking why something is or isn't on screen. `Frame.RequestInspection()`
asks for the first frame whose `FrameEvent` is published after it, one at a
time: a request while one is pending joins it. render publishes
`InspectedEvent{Beat}` once that frame is presented, and
`Frame.ReadInspection(event)` answers it while the publication's jobs run.

```go
func requestInspection(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame) kernel.NoResult {
	frame.RequestInspection()
	return kernel.NoResult{}
}

func countDraws(k *kernel.Kernel, event render.InspectedEvent, frame render.Frame) kernel.NoResult {
	inspection, err := frame.ReadInspection(event)
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	draws := 0
	for _, list := range inspection.CommandLists {
		for _, pass := range list.Passes {
			draws += len(pass.Draws)
		}
	}
	k.Logger().LogDebug("frame inspected", "tick", inspection.Beat.Tick, "lists", len(inspection.CommandLists),
		"draws", draws, "pipelines made", len(inspection.MadePipelines), "reports", len(inspection.Reports))
	return kernel.NoResult{}
}
```

An `Inspection` holds the frame's command lists by order, each with its
label and its passes, compute passes, copies and readbacks, each with its
`Position` in the list; every draw with its `InspectedMaterial` (shader,
entry points, render state and parameters), its pass and draw parameters,
each an `InspectedParameter` by name, group and binding with the value it
bound or `Default`, its instance buffers and counts; what the frame loaded,
made and wrote; the temporaries it took and the materials it versioned; the
pipelines and bind groups it made, the hitches of first draws; its reports;
and its pass timings when it was timed. Every other frame pays one check a
call and allocates nothing, so a tool may stay attached.

## Pass timings

`Frame.SetPassTimings(true)` times every pass and compute pass from the next
frame on, render's own included, until it is turned off. For each frame with
a pass, render publishes `PassTimingsEvent{Beat}` once the GPU has run it, a
frame or more later, and `Frame.ReadPassTimings(event)` answers each pass's
`PassTiming`: its list's order, its label and its GPU duration, by order and
within a list in the order begun. They are render's own, so a task copies
what it keeps.

```go
func setPassTimings(k *kernel.Kernel, on bool, frame render.Frame) kernel.NoResult {
	frame.SetPassTimings(on)
	return kernel.NoResult{}
}

func logSlowPasses(k *kernel.Kernel, event render.PassTimingsEvent, frame render.Frame) kernel.NoResult {
	timings, err := frame.ReadPassTimings(event)
	if err != nil {
		return kernel.NoResult{}
	}
	for _, timing := range timings {
		if timing.Duration > 2*time.Millisecond {
			k.Logger().LogWarn("slow pass", "label", timing.Label, "order", timing.Order, "duration", timing.Duration)
		}
	}
	return kernel.NoResult{}
}
```

Timings need timestamp queries: gfx's `Config.RequiredFeatures` holding
`gfx.FeatureFlagTimestampQuery`, on a backend that has them. Until the wgpu
fork exposes them, none does but `rendertest`'s fake GPU, so turning them on
is reported once, with `ReportKindPassTimingsUnavailable`, and leaves them
off. A frame's passes after its 2048th go untimed, reported with
`ReportKindPassUntimed`. An inspection of a timed frame waits for its timings
and holds them.

## Hot reload

`Inventory.ReloadChanged()` reloads every loaded texture, buffer and shader
whose file changed since it was loaded, by `fs.Stat`'s modification time and
size, reading no file that did not. A shader's files are its own and every
file its preprocessing included, so editing a shared include reloads every
variant using it. A game binds it to a key, or a dev plugin calls it every
second; it allocates only while it runs.

```go
func subscribeReloadKey(configurator *kernel.Configurator) {
	reload := configurator.RegisterTask1(reloadChanged)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.KeyEvent) (platform.KeyEvent, kernel.JobHandle) {
		if event.Key != platform.KeyF5 || !event.Pressed {
			return event, in
		}
		return event, ex.Schedule(reload, kernel.NoArg{}, in).Handle()
	})
}

func reloadChanged(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory) kernel.NoResult {
	inventory.ReloadChanged()
	return kernel.NoResult{}
}
```

- **Where changes come from.** An embedded file never changes, so a game in
  development mounts its asset folder from disk above its embedded one; a
  memory mount stamps each put.
- **Handles survive.** The new objects are built in the caller's job and
  swapped in at the start of the next frame, so a frame draws every asset
  old or every one new, and nothing holding a handle notices.
- **What follows.** A reloaded shader's pipelines are made again at their
  next draw, and its materials bind their values again by name: a name the
  new shader lacks is dropped, a value that no longer fits draws its default.
  A texture or buffer of a new size gets a new gfx object behind the same
  handle; a mesh whose range no longer fits its buffer skips its draws.
- **A failed reload** keeps drawing the last good version, reported once for
  each change; the next change retries.
- Made assets are not reloaded.

## Reports and errors

A **report** is a mistake render finds at run time: logged once through the
kernel's `LogErr` as a `render.Report{Kind, Err}`, written into the
inspection of the frame that raised it, and carried past. Nothing render
does panics at run time; a composition error, a `Config` of another type,
panics in `Init`.

| Kind | What it is |
|---|---|
| `ReportKindOrderTaken` | a command list at an order another list of the frame has |
| `ReportKindOrderReserved` | a command list at render's own `math.MinInt32` or `math.MaxInt32` |
| `ReportKindListOutsideFrame` | a command list begun while no frame records |
| `ReportKindPassTargetsRefused` | a pass refused for its targets |
| `ReportKindLoadFailed` | a file that could not be read, decoded, preprocessed or compiled |
| `ReportKindReleaseRefused` | a release no acquire or make holds |
| `ReportKindWriteRefused` | a write that does not fit its buffer or texture |
| `ReportKindShaderRefused` | a shader declaring `@group(3)` or past the device's limits |
| `ReportKindMeshRefused` | a mesh refused when made |
| `ReportKindMaterialRefused` | a material whose entry points its shader lacks or leaves ambiguous |
| `ReportKindPipelineFailed` | a pipeline a draw needs that could not be made |
| `ReportKindDrawSkipped` | a draw skipped for what it names |
| `ReportKindParameterRefused` | a value a parameter cannot bind, drawing its default |
| `ReportKindInstanceBufferRefused` | an instance buffer refused when set |
| `ReportKindPassStateRefused` | a viewport a pass cannot take |
| `ReportKindCopyRefused` | a copy, buffer clear or readback that does not fit |
| `ReportKindDispatchSkipped` | a dispatch skipped for what it names |
| `ReportKindClearAfterDraw` | a pass clearing what an earlier pass of the frame drew |
| `ReportKindPassTimingsUnavailable` | pass timings turned on without timestamp queries |
| `ReportKindPassUntimed` | the passes of a timed frame past its 2048th |

Calls answering an error answer render's own: `ErrCaptureBusy`,
`ErrCaptureUnsupported` and `ErrDeviceLost`, so a user names no gfx error.
A gfx object gfx refuses to make, such as a texture past the device's
limits, is logged and answers the zero handle.

## Testing with rendertest

A good test of a plugin that draws asserts what reached the GPU, in render's
words, driving the plugin the way the game does: through the kernel and
render's parameters.

### The harness

`rendertest.NewHarness(t)` runs render in a real kernel on a fake GPU.
`Spawn(config, plugins...)` spawns platform on headless windows, metronome
with a render track on a `ManualClock`, gfx on its recording adapter, render
with `config`, and the test's own plugins after render. `Step` draws one
beat and returns once it has finished, its frame presented. The fake GPU is
a frame behind: it finishes the work submitted in one beat when the next
`Step` begins, so a readback lands a beat after its submit.

The harness is strict: the test fails on every error the kernel logs,
checked after each `Step` and at its end, unless it is a report of a kind
the test named with `ExpectReport`. `Reports` answers every report logged.

The tests below test the quick start's game: `newScenePlugin(files)`, beside
it in package `main`, answers `&game{files: files}`. The game opens its own
window, which the harness's headless windows serve:

```go
func TestTheSceneDrawsTheQuadWithItsTexture(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.Spawn(nil, newScenePlugin(os.DirFS("assets")))
	for range 3 {
		h.Step()
	}

	frames := h.Frames()
	lists := frames[len(frames)-1].CommandLists
	if len(lists) != 1 || lists[0].Label != "scene" || len(lists[0].Passes) != 1 {
		t.Fatalf("the last frame recorded %+v, want the scene's one list of one pass", lists)
	}
	draws := lists[0].Passes[0].Draws
	if len(draws) != 1 || draws[0].IndexCount != 6 {
		t.Fatalf("the scene pass drew %+v, want one draw of 6 indices", draws)
	}
	if picture, ok := findParameter(draws[0], "picture"); !ok || picture.Default {
		t.Errorf("the draw bound picture as %+v, want the crate's texture", picture)
	}
}

func TestAMissingTextureIsReportedOnceAndTheQuadStillDraws(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	files := fstest.MapFS{}
	for _, name := range []string{"quad.wgsl", "quad.wgsl.json"} {
		data, err := os.ReadFile(filepath.Join("assets", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	h.Spawn(nil, newScenePlugin(files))
	for range 3 {
		h.Step()
	}

	if reports := h.Reports(); len(reports) != 1 || reports[0].Kind != render.ReportKindLoadFailed {
		t.Errorf("reports %v, want the missing texture's alone", reports)
	}
	frames := h.Frames()
	if lists := frames[len(frames)-1].CommandLists; len(lists) != 1 || len(lists[0].Passes[0].Draws) != 1 {
		t.Errorf("the last frame recorded %+v, want the quad drawn in magenta", lists)
	}
}
```

`OpenWindow(size)` opens a headless window for a plugin that draws into one
it is given, and returns once gfx has made its surface.
`LengthenNextAcquire(d)` makes the next acquire take `d` longer by the
clock, for a test of `AcquireWait`.

### Binding declarations and cmd/declarebindings

gfx's recording adapter parses no WGSL, so the harness answers a shader's
reflection from its **binding declaration**, `lit.wgsl.json` beside
`lit.wgsl`, whose format rendertest's package documentation gives. A shader
compiled with no declaration fails the test, so a plugin embeds each
declaration beside its shader. `cmd/declarebindings` generates them from the
shaders themselves, through render's real path on wgpu's software adapter,
from a `//go:generate` line beside the shaders, a `-variant` for each define
set:

```go
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -variant SKINNED,JOINTS=4u lit.wgsl unlit.wgsl
```

Run `go generate` again whenever a shader's bindings, entry points or inputs
change; a pixel test catches a declaration gone stale.

### Reading a frame back

`Frames` answers each frame gfx has drawn so far as a `render.Inspection`,
the same type an inspection answers, read back from the recording adapter
with the beat of the `Step` that drew it and the reports logged during that
`Step`: its command lists by order with every encoder gfx submitted, their
passes, draws and parameters by name, the dynamic offsets resolved from the
arena bytes written, a copy into a staging buffer as a readback, and what
render made and wrote. What gfx's recording cannot show it leaves empty:
the temporaries, the versioned materials, each draw's `Material` handle and
the pass timings. render's closing list is left out; its mip list is read
as any other.

```go
func findParameter(draw render.InspectedDraw, name string) (render.InspectedParameter, bool) {
	for _, parameter := range slices.Concat(draw.PassParameters, draw.Material.Parameters, draw.DrawParameters) {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return render.InspectedParameter{}, false
}
```

### Pixel tests

A pixel test calls `UseSoftwareAdapter` before `Spawn` to draw on wgpu's
software adapter, real rendering on the CPU with no GPU, and reads its
texels back through render's readback; it also fails on a stale binding
declaration. Its package's `TestMain` calls `rendertest.RunMain`, which
keeps the process's main thread for gogpu, as macOS requires. rendertest's
package documentation says where a pixel test is skipped.

```go
func TestMain(m *testing.M) { rendertest.RunMain(m) }

func TestTheTintShaderFillsItsTargetWithItsTint(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.UseSoftwareAdapter()
	plugin := &tintPlugin{}
	h.Spawn(nil, plugin)
	// The texels arrive a frame or more after the frame drawing them.
	for range 10 {
		h.Step()
		if texels := plugin.texels(); texels != nil {
			if want := []byte{255, 128, 0, 255}; !bytes.Equal(texels[:4], want) {
				t.Errorf("the first texel is %v, want %v", texels[:4], want)
			}
			return
		}
	}
	t.Fatal("no readback was delivered within ten beats")
}

// tintPlugin draws tint.wgsl into a 4×4 texture in its first frame, reads
// the texture back, and keeps its texels.
type tintPlugin struct {
	mutex sync.Mutex
	read  []byte
}

func (*tintPlugin) Name() kernel.PluginName { return "tint" }

func (*tintPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *tintPlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: os.DirFS("testdata")})
	drawn := false
	draw := configurator.RegisterTask2(drawTint)
	// keep hands the texels to the test, outside the kernel, so it stays a
	// closure.
	keep := configurator.RegisterTask1(func(k *kernel.Kernel, event render.ReadbackEvent, frame render.Frame) kernel.NoResult {
		if data, err := frame.ReadbackData(event); err == nil {
			p.mutex.Lock()
			p.read = slices.Clone(data)
			p.mutex.Unlock()
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		if drawn {
			return event, in
		}
		drawn = true
		return event, ex.Join(in, ex.Schedule(draw, kernel.NoArg{}, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.ReadbackEvent) (render.ReadbackEvent, kernel.JobHandle) {
		return event, ex.Schedule(keep, event, in).Handle()
	})
	return nil
}

func (*tintPlugin) Start(*kernel.Kernel) error { return nil }
func (*tintPlugin) Stop(*kernel.Kernel) error  { return nil }
func (*tintPlugin) Destroy() error             { return nil }

// texels answers the texels read back, nil until they arrive.
func (p *tintPlugin) texels() []byte {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.read
}

// drawTint draws tint.wgsl into a 4×4 texture and reads the texture back. A
// test may load in its recording task; a game loads behind a loading screen.
func drawTint(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, frame render.Frame) kernel.NoResult {
	material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "tint", Shader: inventory.AcquireShader("tint.wgsl")})
	target := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "target", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
	list := frame.BeginCommandList("tint", 1)
	pass := list.BeginPass(render.PassDescriptor{Label: "tint", ColorTargets: []render.ColorTarget{{Texture: target, Clear: true}}})
	pass.SetMaterial(material)
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
	pass.End()
	list.ReadTexture(target, render.TextureRegion{})
	list.End()
	return kernel.NoResult{}
}
```

`testdata/tint.wgsl`, its declaration generated beside it by a
`//go:generate` line in the test file:

```wgsl
@vertex
fn vertex_main(@builtin(vertex_index) index: u32) -> @builtin(position) vec4<f32> {
	// One triangle covering the target.
	let corner = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
	return vec4<f32>(corner * 2.0 - 1.0, 0.0, 1.0);
}

@fragment
fn fragment_main() -> @location(0) vec4<f32> {
	return vec4<f32>(1.0, 128.0 / 255.0, 0.0, 1.0);
}
```

### The allocation gate

Nothing render does per frame allocates once its pools have grown, but for
the fixed counts a material version and a pass given a moved temporary
range as a `@group(0)` storage parameter make. render's own gate,
`rendertest/allocationgate_test.go`, checks it with
`testing.AllocsPerRun(...) == 0` over whole beats, the harness's `Step`, on
gfx's recording adapter recording nothing; design.md's *Performance* gives
what it covers and the time per draw against gfx, and render's `AGENTS.md`
how a change to the per-frame path is checked. The harness's recording
adapter keeps every call it records, which allocates, and only rendertest's
own tests can turn that off, so the harness cannot yet gate the allocations
of a plugin of your own.

## Rules

- **Never reach gfx from a plugin that draws**: go through `Inventory`,
  `Frame` and `Surface`, and subscribe to render's events.
- **Record from tasks scheduled after `FrameEvent.Ready`**, joined into the
  handler's out dependency.
- **One command list per recording task**, at an order named in one place,
  with gaps; `math.MinInt32` and `math.MaxInt32` are render's.
- **Keep handles, never a task's `Inventory`, `Frame` or `Surface`.**
- **Release what you acquire or make, once each**, a mesh's and a material's
  buffers, textures and shaders included.
- **Never keep a temporary past its frame**, and hand a temporary texture
  between tasks through state of your own, acquired in a task ordered before
  those that use it.
- **Acquire shaders and warm pipelines behind a loading screen.**
- **Sort draws by shader, then material, then mesh**, since a pass
  remembers its last four materials and its last mesh.
- **Change a material in the task that records its draws**, or in one
  ordered before the frame's recording.
- **Put what changes per draw in draw parameters**, not material setters.
- **Put data a whole pass reads in a made buffer** written with
  `WriteBuffer`, not a temporary range in `@group(0)`.
- **Copy a temporary buffer to change data between passes**: writes land
  before the whole frame.
- **Lay a buffer's rows 256 bytes apart** for a copy to or from a texture.
- **Mirror WGSL structs exactly**, padding included, with `_` fields, and
  check those no uniform setter checks with `CheckLayout`.
- **Leave `@group(3)` alone.**
- **Encode sRGB in the last pass into a `BGRA8Unorm` window**, as gogpu's
  are.
- **Clear only targets of your own.**
- **Read captures, readbacks, inspections and pass timings in tasks joined
  into their event's out dependency**, and copy what you keep.
- **Refill made buffers and textures on `RecoveredEvent`.**
- **Save paths, not handles.**
- **Mount the asset folder from disk above the embedded one** to reload in
  development.
- **In tests, name every report you expect**, and keep a binding declaration
  beside every shader.
