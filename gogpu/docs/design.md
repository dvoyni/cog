# gogpu: design

`github.com/dvoyni/cog/gogpu` is the plugin that implements
[gfx](../../gfx/docs/design.md) and [platform](../../platform/docs/design.md)
over our gogpu fork in `submodules/gogpu`. It binds gfx's backend seam and
platform's window and input seams, and drives gogpu through `HostLoop` from
the kernel's own main-thread tick: gogpu's `App.Run`, its render thread and
its `OnDraw` never run. gfx keeps its handle tables, frame chain, surfaces,
capture and recovery; the plugin passes each call through to gogpu's own
windows, surfaces and device.

Status: designed, not yet built. The decisions come from the
[gogpu map](https://github.com/dvoyni/cog/issues/611); each section links
the ticket that holds its detail. The loop API it rests on, `HostLoop`, was
built on the [loop map](https://github.com/dvoyni/cog/issues/612) and is in
the fork's `main`. The vocabulary is in [`CONTEXT.md`](../CONTEXT.md).

## Constraints

- **One plugin, both halves.** It binds `gfx.InstanceSocket`,
  `platform.WindowsSocket` and `platform.InputSourceSocket`, because the
  windows it opens are the ones whose surfaces gfx draws to.
- **gfx is not reworked for it.** gfx stays a mirror of wgpu with its own
  frame chain. The plugin adapts gogpu to gfx's seam, not the other way
  round ([where the seam between gfx and gogpu sits](https://github.com/dvoyni/cog/issues/712)).
- **Imports:** kernel, gfx, platform, our gogpu fork, and gogpu/wgpu at the
  version the fork pins (v0.34.2, naga v0.19.0). Its own files import the
  fork's `gogpu` package under an alias.
- **Targets:** macOS on Metal, Windows on Vulkan and DX12, Linux on Vulkan
  over X11 and Wayland. Never GL: its context is bound to one thread.
- **The application spawns from the main thread,** as the kernel's documented
  `main` does. `HostLoop` requires it, and it is documented, not checked.
- **Fork changes go upstream.** Every change the plugin needs in the fork,
  goffi or wgpu is proposed upstream as a PR. Until they merge, the plugin
  builds against the forks.

## Shape

([gogpu plugin: its module, start, backend choice, device recovery and tests](https://github.com/dvoyni/cog/issues/715))

| Type | Kind | What it is |
|---|---|---|
| `Plugin` | plugin, `gogpu.NewPlugin()` | Listed after gfx and platform. Binds the three sockets and pins gfx's surface tasks. |
| `Config` | config, `*gogpu.Config` | `Backend`, overriding gogpu's choice per OS; `MainWindow`, the window platform opens at start. |
| `shader` | package `gogpu/shader` | The WGSL preprocessor: `#include`, `#define`, `#const`, `#if`, and the hoisting of `enable`, `requires` and `diagnostic`. |

The module is `cog/gogpu`, its own Go module in `go.work`, beside
`use ./submodules/gogpu` once the module requires the fork.

## Start and the device

([what gogpu's HostLoop needs to start](https://github.com/dvoyni/cog/issues/713),
[the plugin's start](https://github.com/dvoyni/cog/issues/715))

- **`HostLoop.Start` runs lazily,** at the first backend call: gfx's
  `RequestAdapter`, from gfx's `Start`, on the goroutine that called `Spawn`,
  which is main.
- **It starts with no window.** The fork's `Start` brings up the platform
  manager and the instance, adapter and device with no surface; on Vulkan,
  DX12 and Metal the device needs none. Windows come later, from platform's
  `Open`.
- **The adapter options reach gogpu's `Config`:**
  - `PowerPreference` and `RequiredFeatures` by cast; gogpu's feature bits
    are gfx's `FeatureFlags`.
  - `RequiredLimits` field by field, the fields gfx's `Limits` lacks taken
    from `gputypes.DefaultLimits()`. gfx passes WebGPU's defaults explicitly,
    since wgpu's zero means the adapter's full limits.
  - `ForceFallbackAdapter`, for wgpu's software adapter.
  - The fork checks the required features and limits against the adapter
    before `RequestDevice`, since Vulkan and Metal ignore them and DX12
    checks only features.
- **The backend** is gogpu's default on each OS, GL excluded. `Config.Backend`
  overrides it; there is no environment variable, and tests set the field.
- **The adapter answers** gogpu's `Info`, `Features` and `Limits`; the device
  answers what was requested.
- **Objects go straight to the device.** Buffers, textures, pipelines and the
  rest are created on the `*wgpu.Device` gogpu provides, from gfx's workers.
  wgpu's device is safe from many goroutines, and `HostLoop`'s frame calls
  touch only gogpu's own objects.
- **The adapter's queue can present.** A surface-less Vulkan adapter picks
  its graphics queue without checking presentation, so the first surface
  configured confirms it.

## The main thread

([how gfx's calls reach the main thread](https://github.com/dvoyni/cog/issues/708),
[which surface calls each OS requires on the main thread](https://github.com/dvoyni/cog/issues/710),
[where the seam sits](https://github.com/dvoyni/cog/issues/712))

gfx pins nothing. The plugin is listed after gfx and platform, reads
`gfx.Tasks` in its `Init`, and pins with `configurator.PinToMainThread` only
what `HostLoop` or the OS requires:

| gfx task | macOS / Metal | Windows / Vulkan | Windows / DX12 | Linux |
|---|---|---|---|---|
| `AcquireFramebuffers` | pinned | pinned | pinned | pinned |
| `PresentFramebuffers` | pinned | pinned | pinned | pinned |
| `ConfigureSurfaces` | — | pinned | pinned | — |
| `ReleaseSurface`, and the release `Stop` schedules | — | pinned | — | — |
| `CreateSurface` | — | — | — | — |
| `RecoverDevice` | pinned | pinned | pinned | pinned |

- **Acquire and present are pinned everywhere** because they go through
  `HostLoop.BeginFrame` and `EndFrame`, which run on the thread that called
  `Start` and span a macOS autorelease pool.
- **Windows needs the rest** because Vulkan's swapchain calls and DXGI may
  send messages to the window, and cog's main thread pumps only inside
  platform's `ProcessEvents`. DX12's acquire and release are unclear and stay
  unpinned until a run on Windows shows they send a message.
- **The surfaces of windows still open at shutdown** are released by a task
  gfx's `Stop` schedules, a sixth field of `gfx.Tasks`, since `Destroy` runs
  off the main thread
  ([gfx: the surfaces of windows still open are released by a task Stop schedules](https://github.com/dvoyni/cog/issues/711)).
- **Device recovery** is a task in `gfx.Tasks` too, pinned on every OS
  ([gfx: device recovery is a task in gfx.Tasks, for a backend to pin](https://github.com/dvoyni/cog/issues/716)).

## Windows

([gogpu platform half: what the fork lacks](https://github.com/dvoyni/cog/issues/718),
[which platform gaps are closed now](https://github.com/dvoyni/cog/issues/719))

- **`Open`** creates the window and its surface through the fork's one method
  that opens any window, the first included: hidden until shown, its
  fullscreen applied by the fork itself on macOS and Windows, which ignore it
  at creation. It reports the window open from the next `Pump`. The
  window's `NativeHandle` is not needed: gfx's surface is gogpu's own.
- **`Change`** applies what differs between two entries through the fork's
  per-window setters: title, size, fullscreen and visibility.
- **The cursor:** `SetCursor`, `SetCursorPosition` and `EffectiveCursorMode`
  through the per-window cursor shape, mode and position, the mode answered
  as the one in force.
- **`Close`** destroys the window at once through the fork's public
  `Window.Destroy`.
- **`Pump`** is `HostLoop.PumpEvents`, on the main thread inside platform's
  `ProcessEvents`. Each window's resize is recorded there and applied at its
  `BeginFrame`, so no surface is reconfigured outside gfx's lock.
- **The user's close** is reported and the window kept: closing is the
  application's, and gogpu's quit on the last window closed is off.
- **The main window** is `Config.MainWindow`, opened at start through
  platform's window request like any other window.

## Input

([gogpu platform half: what the fork lacks](https://github.com/dvoyni/cog/issues/718),
[which platform gaps are closed now](https://github.com/dvoyni/cog/issues/719))

- **Every `InputChange` is built from the per-window callbacks** on gogpu's
  `Window`: close, key press and release, text, pointer, scroll, resize, and
  the focus and scale reports the fork adds. gogpu's input queue names no
  window, so it is not used.
- **Keys** map from gogpu's physical keys by a table, dense on cog's side.
  Modifiers convert by cast, Caps and Num Lock included.
- **Repeats** arrive as further key-downs on every OS. The plugin flags a press
  of a key it already holds.
- **Key labels** come from the fork's label call, and a layout change from its
  layout-change report, on all four systems.
- **Text** is what the layout, dead keys and Compose type, on every system.
  Committed text from the IME, the emoji picker and dictation arrives on
  Windows only.
- **The pointer:** moves, enter and leave, buttons, the implicit capture in a
  drag, and locked motion as `PointerDelta`.
- **Scroll** arrives in lines or points, Wayland's wheel notches as lines.

## Surfaces and frames

([where the seam sits](https://github.com/dvoyni/cog/issues/712),
[gfx: automatic visual checks from frame captures](https://github.com/dvoyni/cog/issues/717))

- **`CreateSurface`** attaches to the surface gogpu made for the window;
  `ReleaseSurface` lets it go with the window.
- **`SurfaceCapabilities`** and **`ConfigureSurface`** are gogpu's, applied to
  that window's surface.
- **`AcquireFramebuffer`** is `HostLoop.BeginFrame`: it applies the window's
  latest size, observes Wayland's frame gate, and stores the surface texture
  and its view at gfx's indices. Nothing to draw to is gfx's `Timeout`, an
  outdated or lost surface its `Outdated` or `Lost`, and an error is answered,
  not logged.
- **`Present`** is `HostLoop.EndFrame`, its error answered.
- **A captured frame is drawn offscreen.** When gfx's acquire says the window
  is captured that frame, the plugin hands gfx an offscreen texture of the
  surface's format and size instead, which gfx draws into and copies out. At
  present the plugin draws it onto the surface with a full-screen pass, then
  calls `EndFrame`. The texture is made at the window's first capture and
  kept until it resizes or closes. A frame not captured costs nothing extra,
  and every backend can capture, DX12 and the software adapter included
  ([gfx: a capture is taken from the frame acquired after the request](https://github.com/dvoyni/cog/issues/721),
  [gogpu plugin: a captured frame is drawn offscreen, then onto the window](https://github.com/dvoyni/cog/issues/722)).
- **FIFO blocks at acquire,** up to about one refresh, never longer: on Metal
  the longest `BeginFrame` measured 18 to 27 ms at 60 frames a second
  ([run the v0.34.x Metal changes on a Mac](https://github.com/dvoyni/cog/issues/723)).

## Shaders

([gfx: move shader preprocessor to implementation](https://github.com/dvoyni/cog/issues/709))

- **Preprocessing is the backend's.** `InstanceSocket.PreprocessShader(files,
  path, defines)` runs `gogpu/shader`'s engine over the `fs.FS` it is handed;
  gfx exposes it as `Device.PreprocessShader`. The seam types,
  `ShaderDefine`, `ShaderSourceLocation` and `ErrShaderSource`, stay in gfx. A
  tool with no GPU imports `gogpu/shader` directly.
- **Reflection happens once, at `CreateShaderModule`,** with naga: the plugin
  keeps the result at the module's index, answers an error for invalid WGSL,
  and answers `ReflectRenderPipelineLayout` and `ReflectComputePipelineLayout`
  with the layouts WebGPU's "auto" builds, and `ReflectVertexInputs` with a
  vertex entry point's location inputs
  ([gfx: shader reflection at module creation](https://github.com/dvoyni/cog/issues/694),
  [gfx: checks at pipeline creation](https://github.com/dvoyni/cog/issues/696)).

## Device loss

([what gogpu's HostLoop needs](https://github.com/dvoyni/cog/issues/713),
[the plugin's device recovery](https://github.com/dvoyni/cog/issues/715))

- **Detection:** on Vulkan, wgpu's typed `ErrDeviceLost` at acquire, present
  and fence waits; on DX12, a removed, hung or reset device found in the
  wrapped HRESULT. Both answer gfx's `ErrDeviceLost`.
- **Replacement keeps the windows.** The fork releases gogpu's device-level
  objects and the device, requests a device from the same or a fresh adapter
  with the same descriptor, and marks every surface unconfigured. gfx's
  recovery task, pinned to the main thread, calls it under the write lock on
  `gfx.Device`, then recreates its objects on the device gogpu now provides.
- **Metal never detects a loss,** so recovery never runs there. It is a
  backend gap, recorded in gfx's design.

## The fork changes

([the device changes](https://github.com/dvoyni/cog/issues/713),
[the platform changes](https://github.com/dvoyni/cog/issues/719))

Each is its own upstream PR, stacked on the window-less start, its title and
description approved by the owner before it is opened. A behaviour change is
described in the PR, not in the fork's `CHANGELOG.md`.

- **Start with no window**, and one method that opens any window with its
  surface.
- **Device options:** `RequiredLimits` and `ForceFallbackAdapter` in `Config`,
  the features and limits checked against the adapter.
- **Frame errors:** `BeginFrame` and `EndFrame` answer them.
- **Device replacement** that keeps the windows.
- **Resizes applied at `BeginFrame`** for every window.
- **No live-resize hook under `HostLoop`:** today it draws `App`'s own frame
  from inside `PumpEvents` during a macOS live resize
  ([gogpu fork: HostLoop draws App's own frame from inside PumpEvents during a macOS live resize](https://github.com/dvoyni/cog/issues/714)).
- **Per-window operations:** title, fullscreen, cursor shape, mode and
  position, and scale.
- **Per-window reports:** focus, and scale changes, which Wayland detects at
  the pump.
- **A public `Window.Destroy`** that works on every system.
- **macOS:** `PollEvents` routes each event to its own window; the hidden
  cursor is balanced with unhide; Caps Lock reaches the modifiers.
- **The cursor mode in force,** answered on Windows, X11 and Wayland.
- **Key labels and layout changes** on all four systems.
- **Dead keys and Compose** on X11 and Wayland, through libxkbcommon's compose
  functions.
- **Wayland scrolls in lines.**

Beside the gogpu fork:

- **goffi:** cgo builds on Linux call `dlopen` through cgo, so the fork links
  under `-race`
  ([goffi fork: cgo builds on Linux call dlopen through cgo, so gogpu links under -race](https://github.com/dvoyni/cog/issues/725)).
- **wgpu:** a texture sampled through a bind group gets no barrier from its
  previous use on Vulkan. The fix starts from the first failing
  render-to-texture-then-sample test, in `dvoyni/wgpu` joined as a submodule,
  and must land before scene starts shadows or post effects. The offscreen
  pass of a captured frame depends on it too.

## Documented limits

- **Wayland supports one window.** A second fails to open with an error.
- **Committed text from the IME, the emoji picker and dictation** works on
  Windows only. On macOS, X11 and Wayland it waits for the IME composition
  effort.
- **Metal never reports a device loss.**
- **A Windows modal loop freezes the frame** while a window is dragged or
  resized, as decided on the loop map.
- **`-race` on Linux needs the goffi fork** until its PR merges.

## Testing

Three seams, no new one:

- **The plugin, through gfx's and platform's public API.** A kernel spawned
  with gfx, platform and gogpu on a real GPU and display. The tests assert
  only through gfx's `Device` and events and platform's `WindowManager`,
  `InputState` and events, and skip when no GPU or display is present.
  - At first they run by hand on the Mac, the Lima VM (Xvfb and headless
    sway) and Windows.
  - On wgpu's software adapter, through `ForceFallbackAdapter`, they draw a
    known scene, capture it with `RequestCapture` and check `ReadCapture`'s
    `image.NRGBA`. Neither gfx nor the plugin compares frames: the test does.
- **The fork, at `HostLoop` and `Window`.** Each fork change carries its own
  tests in the fork, since each goes upstream as a PR.
- **gfx, through its harness on the recording adapter,** for the gfx-side
  changes: the surface release `Stop` schedules, the recovery task, shader
  preprocessing through the socket, and the capture from the frame acquired
  after the request.

The suites pass under `-race`. Builds are checked with `CGO_ENABLED=0` and
`1`, as in [the WSL run](https://github.com/dvoyni/cog/issues/724).

## Out of scope

- **GLES and the web**, deferred on gfx.
- **Device-loss detection on Metal,** which needs a wgpu change.
- **Wayland with many windows:** one shared connection, per-window
  operations, hiding by attaching no buffer.
- **IME, emoji and dictation commits on macOS, X11 and Wayland,** and IME
  composition everywhere: they return with the IME composition effort.
