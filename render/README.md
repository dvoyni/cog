# render

`github.com/dvoyni/cog/render` is the immediate-mode rendering API between
[gfx](../gfx) and the plugins that draw: drawbridge, 2D, a debug renderer, an
immediate-mode UI. It is its own Go module (Go 1.27) and depends on nothing
but [kernel](../kernel), [metronome](../metronome), [platform](../platform)
and gfx. Its users never reach gfx. It knows nothing of cameras, lights,
culling or the ECS, and ships no materials.

## Features

- **Assets by path, ready in the frame that asks.** Shaders, PNG and JPEG
  textures and buffers load from platform's files, the GPU object made when
  the call returns and its contents written before the frame's submit, with
  no loading state. A second load of the same path and options shares the
  first, counted, and the last release frees it at the end of its frame.
- **Pointer-free handles.** Every asset is a comparable handle an ECS
  component can hold. A failed texture draws magenta, and a draw of a failed
  shader, mesh or buffer is skipped, each reported once.
- **Binding by name.** A material's, a pass's and a draw's values bind to a
  shader's WGSL names, their level following the group: `@group(0)` for a
  pass, `@group(1)` for a material, `@group(2)` for a draw. A Go struct is
  checked against its WGSL block, and an unbound binding draws a fixed
  default.
- **Layouts, pipelines and bind groups are render's.** It builds and shares
  them, and a pass's and a draw's uniform blocks go in a per-frame arena, so
  draws setting them make no bind group.
- **Command lists filled in parallel.** Each recording task fills a list of
  its own at an order it chooses, of passes, compute passes, copies and
  readbacks. A duplicate order and a pass clearing what an earlier pass drew
  are reported.
- **Temporaries.** Textures from a pool by descriptor, buffer ranges of a
  per-frame arena, and meshes over them, each for one frame.
- **The frame's other work.** Mips, capture, readback, the surface request,
  device recovery, hot reload of changed files and their includes, frame
  inspections for agents, and per-pass GPU timings.
- **Tested without a GPU.** `rendertest` runs render in a real kernel on
  gfx's recording adapter and reads each frame back in render's words, or
  draws on wgpu's software adapter for pixel tests.
- **Zero allocations per frame** once its pools have grown, which the tests
  assert, but for a fixed count for each material changed after its draw in
  the frame and each pass given a moved temporary range as a `@group(0)`
  storage parameter.

## Shape

- **`Plugin`**, from `NewPlugin()`, is render. **`Config`** is its part of
  the kernel's config: how long pooled things stay unused before they are
  freed, and the arena's chunk size.
- **`FrameEvent`** is the frame its users record, published inside each of
  gfx's `DrawEvent`s. A plugin that draws schedules its recording tasks after
  its `Ready` and joins them into the out dependency it returns.
- Three **task parameters** declare gfx's and platform's locks for their
  user: **`Inventory`** reaches the assets, **`Frame`** the frame being
  recorded, and **`Surface`** the windows' surfaces.
- **`Texture`**, **`Buffer`**, **`Shader`**, **`Mesh`** and **`Material`**
  are the handles. A **`CommandList`** holds **`Pass`**es and
  **`ComputePass`**es.
- **`Report`** is a mistake render finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `EndFrame`,
  `ReloadAssets`, `DeliverCapture`, `DeliverReadback`), to order handlers
  around; a private frame state and a private asset table.
- **Resources it reads from other plugins:** `gfx.Device`, for read through
  `Frame` and `Inventory` and for write through `Surface`; `platform.FS`, for
  read through `Inventory`.
- **Task parameters it offers:** `Inventory`, `Frame` and `Surface`.
- **Events it publishes:** `FrameEvent`, `RecoveredEvent`, `CapturedEvent`,
  `ReadbackEvent`, `InspectedEvent` and `PassTimingsEvent`, each inside its
  handler of a gfx event.
- **Events it subscribes to:** `gfx.DrawEvent`, `gfx.RenderEvent`,
  `gfx.DeviceRecoveredEvent`, `gfx.CapturedEvent` and `gfx.BufferMappedEvent`.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets under
  the prefix `cog.render`, at the lowest priority.

render is listed after gfx and platform, and a plugin that draws is listed
after render.

See [docs/README.md](docs/README.md) for the guide with examples,
[docs/design.md](docs/design.md) for the design and why it is this way, and
[CONTEXT.md](CONTEXT.md) for its words.
