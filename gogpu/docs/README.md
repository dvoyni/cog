# gogpu: guide

How an application runs on the gogpu plugin. The design behind it is in
[`design.md`](design.md).

## Spawning

The application spawns its kernel from the main thread, with metronome,
platform and gfx listed before gogpu:

```go
func init() { runtime.LockOSThread() }

func main() {
	s := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{Tracks: tracks},
		gogpu.PluginName:     &gogpu.Config{MainWindow: platform.WindowEntry{Title: "My game", Size: platform.Size{Width: 1280, Height: 720}}},
	}
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0), // on the main thread
		metronome.NewPlugin(),
		platform.NewPlugin(),
		gfx.NewPlugin(),
		gogpu.NewPlugin(),
		game,
	); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
}
```

- **Main thread.** gogpu starts its platform and adapter at gfx's first
  call, and requests its device at the next, both inside `Spawn`, and every
  window and frame call runs on the thread that started it, as does its stop
  at the kernel's teardown. Spawning from another goroutine is not checked
  and fails on macOS.
- **Order.** gogpu reads `gfx.Tasks` in its `Init` to pin them, so it comes
  after gfx and platform.

## Configuring

The plugin's part of the kernel's config is a `*gogpu.Config`:

- **`Backend`:** leave it zero for gogpu's choice on each OS: Metal on macOS,
  Vulkan on Linux and Windows. Set it to try DX12 on Windows, or in a test.
- **`MainWindow`:** the window opened at start, as a `platform.WindowEntry`.
  Zero opens none; the application opens its windows through
  `platform.WindowManager`.

The device is gfx's to configure: `gfx.Config`'s `PowerPreference`,
`RequiredFeatures` and `RequiredLimits` reach gogpu, and
`ForceFallbackAdapter` picks wgpu's software adapter, for pixels without a
GPU.

## Shaders

Preprocess WGSL through the device, from a task holding `kernel.Read[gfx.Device]`
and `kernel.Read[platform.FS]`, then create the module from the text:

```go
source, err := device.PreprocessShader(files.Get(), "shaders/lit.wgsl", defines)
```

A failed directive answers a `gfx.ErrShaderSource`, naming the file and line.

## Capturing a frame

`RequestCapture(window)` captures the first frame acquired after the
request, on every backend. `CapturedEvent` arrives a frame later, and
`ReadCapture` answers its `image.NRGBA`. Comparing it with anything is the
caller's work. The plugin draws a captured frame into a texture of its own,
made for that frame, then onto the window, so the window shows what was
captured. A capture is a one-off request: one a frame works, but makes and
frees a window-sized texture each time.

## Limits

- Wayland supports one window; a second fails to open.
- Committed text from the IME, the emoji picker and dictation arrives on
  Windows only.
- Metal never reports a device loss, so gfx never recovers there.
- On Windows the frame freezes while a window is dragged or resized.
