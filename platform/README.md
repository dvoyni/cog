# platform

`github.com/dvoyni/cog/platform` is what the operating system provides to a
Cog kernel: windows, input and filesystems. It is its own Go module
(Go 1.27) and depends on nothing but the standard library and
[kernel](../kernel). It holds no platform code: it declares interfaces that
implementing plugins fill, and owns the logic that is the same for every
implementation. Its [`filesystems`](filesystems) package holds the writable
filesystems an application binds.

## Shape

- **`WindowsSocket`** and **`InputSourceSocket`** are the window and input
  seams, which the implementing plugins bind, gogpu's among them. Each takes
  at most one binding; a kernel without them applies no window request and
  reports no input.
- **`Window`** is an opaque, pointer-free handle from a counter that is never
  reused. A handle kept past its window's close names nothing.
- **`*WindowManager`** holds every window: what the application wants it to
  be, set whole with `Change` or a field at a time with `SetTitle`,
  `SetSize`, `SetFullscreen` and `SetHidden`, its cursor, a field at a time
  with `SetCursorShape`, `SetCursorHidden` and `SetCursorMode`, and what it
  currently is, the effective cursor mode, `EffectiveCursorMode`, among it.
  `SetCursorPosition` moves the cursor on the next pump run.
- **`CursorRequest`** is a window's cursor: a **`CursorShape`**, whether it
  is hidden, and a **`CursorMode`**, Normal, Confined or Locked. While
  locked, the pointer position stays and `PointerMoveEvent`'s delta carries
  the motion.
- **`Tasks`** holds **`ProcessEvents`**, the pump, a task pinned to the main
  thread.
- **`Handlers`** holds the `kernel.Sub` of every handler platform
  subscribes, `FlushValues`, so a plugin listed after platform orders its
  own around it.
- **`*InputState`** holds the input levels as of the last pump: the keys
  held, read with `IsHeld`, the current layout's key labels, `Label`, each
  window's pointer, `PointerPosition` in window space and `IsPointerInside`,
  and the last pump run's edges, `JustPressed` and `JustReleased`, correct
  only in the update that ran the pump.
- **`Key`** names a position on a US keyboard whatever the layout, or a
  mouse button, in one space; it prints as its name (`"KeyW"`) and
  `ParseKey` reads it back. **`Modifiers`** is the bitmask a key change
  carries.
- **`WindowOpenedEvent`**, **`WindowClosedEvent`**,
  **`CloseRequestedEvent`**, **`KeyEvent`**, **`TextEvent`**,
  **`FocusEvent`**, **`LayoutChangedEvent`**, **`PointerMoveEvent`**,
  **`ScrollEvent`**, **`PointerEnterEvent`**, **`PointerLeaveEvent`** and
  **`ResizeEvent`** are what the pump publishes, in the order the changes
  happened. A run's pointer moves in one window coalesce into one
  `PointerMoveEvent`. Losing focus releases every held key.
- **`WriteFSSocket`** is the storage seam, taking at most one binding: a
  writable filesystem from [`filesystems`](filesystems),
  `NewDiskWriteFS(directory)` on desktop or `NewLocalStorageWriteFS(key)` in
  a browser. Without one, every write fails with **`ErrNoWriteFS`**. Any
  plugin binds read-only **`ReadMountSocket`**s, each any `fs.FS` at a
  priority, such as an `embed.FS` or an `os.Root`'s `FS`.
- **`FS`**, an alias of `fs.FS`, is the overlay resource: the writable
  filesystem first, then the read mounts by priority, listings merged. It
  only reads; under its write lock, **`ElevateFileAccess`** gives the
  **`WriteFS`** that writes, into the writable filesystem.
- **`*Values`** keeps the values, a JSON file in `FS`, `config.json`
  unless `Config` names another. Its `Get` takes the `FS`; `Set`, `Delete`
  and `Flush` take the `WriteFS`. The file is read once and kept; changes
  are written by `Flush`, and by platform when a window closes.

## Kernel surface

- **Resources it installs:**
  - `Tasks`: `ProcessEvents`, the pump, for the application to schedule.
  - `Handlers`: `FlushValues`, to order handlers around.
  - `FS`: the files, read under its read lock and written through
    `ElevateFileAccess` under its write lock.
  - `*Values`: the key-value store kept in a file in `FS`.
  - `*WindowManager`: the window request and the window state.
  - `*InputState`: the input levels and the last pump run's edges.
- **Resources it reads from other plugins:** none.
- **Events it publishes**, from `ProcessEvents`: `WindowOpenedEvent`,
  `WindowClosedEvent`, `CloseRequestedEvent`, `KeyEvent`, `TextEvent`,
  `FocusEvent`, `LayoutChangedEvent`, `PointerMoveEvent`, `ScrollEvent`,
  `PointerEnterEvent`, `PointerLeaveEvent` and `ResizeEvent`.
- **Events it subscribes to:** `WindowClosedEvent`, `Handlers.FlushValues`,
  which flushes the values after the handlers before it.
- **Sockets it declares:** `WindowsSocket` and `InputSourceSocket`, optional
  and exclusive, for the window implementation and the input source;
  `WriteFSSocket`, optional and exclusive, for the writable filesystem;
  `ReadMountSocket`, optional, for the read mounts.
- **Sockets it binds:** none.

A plugin that reads `Tasks` or `Handlers` in its `Init` is listed after
platform. Plugins binding its sockets go anywhere.

## Wiring

Implementing plugins bind to platform's sockets, so they go anywhere in
`Spawn`. The filesystems are adapters, not plugins: the game's own plugin
binds them. A plugin that reads `platform.Tasks` in its `Init` comes after
platform:

```go
supervisor.Spawn(config, workerCount,
	gogpu.NewPlugin(),    // binds the window implementation and the input source
	platform.NewPlugin(),
	game.NewPlugin(),     // the game's own: binds its filesystems, reads platform.Tasks
)
```

platform schedules `ProcessEvents` nowhere. A handler that schedules it, on
whichever tracks the application chooses, **returns it in its out
dependency**, so the track's in-flight limit sees it; otherwise each beat
queues another pump behind a blocked main thread:

```go
tasks, _ := configurator.Resource[platform.Tasks]()
configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
	return event, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
})
```

`main` keeps ticking the main thread, where the pump runs.

The game's plugin makes its writable filesystem in `Init`, binds it with its
read mounts, and closes it in `Destroy`:

```go
func (p *game) Init(c *kernel.Configurator, _ any) error {
	writable, err := filesystems.NewDiskWriteFS(userDirectory)
	if err != nil {
		return err
	}
	p.writable = writable
	c.BindSocket[platform.WriteFSSocket](writable)
	c.BindSocket(platform.ReadMountSocket{FS: assets}) // its embed.FS
	// ...
}

func (p *game) Destroy() error { return p.writable.Close() }
```

To open a window, a job calls `Open` under the manager's write lock and
gets its `Window` at once; the next pump run opens it and publishes
`WindowOpenedEvent`. `Close` closes it. `CloseRequestedEvent` only asks:
nothing closes until the application calls `Close`.

A job reads files under `kernel.Read[platform.FS]` with the `fs` helpers,
and writes them under `kernel.Write[platform.FS]` through
`platform.ElevateFileAccess`:

```go
load := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Read[platform.FS]) kernel.NoResult {
	data, err := fs.ReadFile(filesystem.Get(), "levels/1.json")
	// ...
	return kernel.NoResult{}
})
save := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Write[platform.FS]) kernel.NoResult {
	err := platform.ElevateFileAccess(filesystem).WriteFile("saves/slot1", data, 0o644)
	// ...
	return kernel.NoResult{}
})
```

See [docs/README.md](docs/README.md) for the guide with examples, and
[docs/design.md](docs/design.md) for the design and why it is this way.
