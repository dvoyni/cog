# platform

`github.com/dvoyni/cog/platform` is what the operating system provides to a
Cog kernel: windows, input and, later, filesystems. It is its own Go module
(Go 1.27) and depends on nothing but the standard library and
[kernel](../kernel). It holds no platform code: it declares interfaces that
implementing plugins fill, and owns the logic that is the same for every
implementation.

## Shape

- **`Windows`** and **`InputSource`** are the seams, installed as resources
  by the implementing plugins, gogpu's among them. Each needs exactly one
  installer.
- **`Window`** is an opaque, pointer-free handle from a counter that is never
  reused. A handle kept past its window's close names nothing.
- **`*WindowManager`** holds every window: what the application wants it to
  be, set whole with `Change` or a field at a time with `SetTitle`,
  `SetSize`, `SetFullscreen` and `SetHidden`, and what it currently is.
- **`Pump`** holds **`ProcessEvents`**, the pump, a task pinned to the main
  thread.
- **`WindowOpenedEvent`**, **`WindowClosedEvent`** and
  **`CloseRequestedEvent`** are what the pump publishes.

## Wiring

List the implementing plugins **before** platform in `Spawn`, since they
install the seams platform's `Init` finds:

```go
supervisor.Spawn(config, workerCount,
	gogpu.NewPlugin(),    // installs platform.Windows and platform.InputSource
	platform.NewPlugin(),
	game.NewPlugin(),     // reads platform.Pump
)
```

platform schedules `ProcessEvents` nowhere. A handler that schedules it, on
whichever tracks the application chooses, **returns it in its out
dependency**, so the track's in-flight limit sees it; otherwise each beat
queues another pump behind a blocked main thread:

```go
pump, _ := configurator.Resource[platform.Pump]()
configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
	return event, ex.Join(in, ex.Schedule(pump.ProcessEvents, kernel.NoArg{}).Handle())
})
```

`main` keeps ticking the main thread, where the pump runs.

To open a window, a job calls `Open` under the manager's write lock and
gets its `Window` at once; the next pump run opens it and publishes
`WindowOpenedEvent`. `Close` closes it. `CloseRequestedEvent` only asks:
nothing closes until the application calls `Close`.

See [docs/design.md](docs/design.md) for the design.
