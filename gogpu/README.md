# gogpu

`github.com/dvoyni/cog/gogpu` is the plugin that gives a Cog kernel a real
GPU and real windows. It implements [gfx](../gfx) and [platform](../platform)
over our gogpu fork in `submodules/gogpu`, driving it through `HostLoop` from
the kernel's main-thread tick. Its `shader` package is the WGSL preprocessor.

Status: designed; of it, only `gogpu/shader` is built. The design is in
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

## Runs on

macOS on Metal, Windows on Vulkan and DX12, Linux on Vulkan over X11 and
Wayland, and wgpu's software adapter anywhere. Wayland supports one window,
and committed IME text works on Windows only.
