# gogpu

`github.com/dvoyni/cog/gogpu` is the plugin that gives a Cog kernel a real
GPU and real windows. It implements [gfx](../gfx) and [platform](../platform)
over our gogpu fork in `submodules/gogpu`, driving it through `HostLoop` from
the kernel's main-thread tick. Its `shader` package is the WGSL preprocessor.

Status: being built: gfx gets gogpu's adapter and device, with every other
device call answering an error naming it, windows open and close through
platform, and `gogpu/shader` preprocesses shaders. The design is in
[`docs/design.md`](docs/design.md), the guide in
[`docs/README.md`](docs/README.md), and the build on the
[gogpu map](https://github.com/dvoyni/cog/issues/611).

## Shape

- **`gogpu.NewPlugin()`**, listed after gfx and platform. It binds
  `gfx.InstanceSocket`, `platform.WindowsSocket` and
  `platform.InputSourceSocket`, and pins the gfx tasks its OS needs on the
  main thread.
- **`gogpu.Config`:** `Backend`, overriding gogpu's choice on each OS, and
  `MainWindow`, the window platform opens at start.
- **`gogpu/shader`:** `Preprocess` over an `fs.FS`, with `#include`,
  `#define`, `#const` and `#if`. gfx reaches it through
  `Device.PreprocessShader`; a tool with no GPU imports it directly.

## Kernel surface

- **Resources it installs:** none.
- **Resources it reads from other plugins:** `gfx.Tasks`, in its `Init`, to
  pin `ReleaseDevice` to the main thread, where the `HostLoop` stops; and
  `*platform.WindowManager`, in its `Init`, to open `Config.MainWindow`.
- **Events it publishes:** none.
- **Events it subscribes to:** none.
- **Sockets it declares:** none.
- **Sockets it binds:** `gfx.InstanceSocket`, with the plugin itself: its
  `RequestAdapter` starts the `HostLoop`, and its `Release` stops it.
  `platform.WindowsSocket`, with the plugin itself: its `Open` opens a gogpu
  window with its surface, its `Close` destroys it, and its `Pump` pumps the
  `HostLoop` and reports a window opened, closed by gogpu, or asked to close
  by the user, which keeps it open. Window changes and the cursor answer an
  error naming them until they are built.

The plugin is listed after gfx and platform, and checks them with
`Dependencies`: it reads their resources in its `Init`.

## Runs on

macOS on Metal, Windows on Vulkan and DX12, Linux on Vulkan over X11 and
Wayland, and wgpu's software adapter anywhere. Wayland supports one window,
and committed IME text works on Windows only.
