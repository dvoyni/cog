# platform guide

How to use `github.com/dvoyni/cog/platform`, with examples. For the reasons
behind each choice, see [design.md](design.md). It assumes the
[kernel guide](../../kernel/docs/README.md), and its examples pace the pump
with [metronome](../../metronome/docs/README.md).

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Windows](#windows)
- [The pump](#the-pump)
- [Input](#input)
- [The cursor](#the-cursor)
- [Files](#files)
- [The filesystems package](#the-filesystems-package)
- [Values](#values)
- [Implementing the window and input seams](#implementing-the-window-and-input-seams)
- [Rules](#rules)

## Concepts

```
window implementation ──┐  WindowsSocket       (gogpu)
input source ───────────┤  InputSourceSocket   (gogpu, later a scripted one)
writable filesystem ────┤  WriteFSSocket       (filesystems.NewDiskWriteFS, …)
read mounts ────────────┘  ReadMountSocket     (an embed.FS, an os.Root's FS)
                              │
                         platform
          ┌───────────────────┼──────────────────────┐
   *WindowManager        *InputState            FS, *Values
   request + state     levels + edges     the overlay + the values
          └─ ProcessEvents, the pump: applies the request, folds the input,
             publishes KeyEvent, PointerMoveEvent, WindowOpenedEvent, …
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. |
| `Config` | platform's part of the kernel's config: `ValuesFile`, `config.json` when empty. |
| `WindowsSocket`, `InputSourceSocket` | The window and input seams an implementing plugin binds. Optional. |
| `WriteFSSocket`, `ReadMountSocket` | The writable filesystem and the read mounts the application binds. Optional. |
| `Window` | An opaque, pointer-free handle naming one window, never reused. |
| `*WindowManager` | Every window: what the application wants it to be, and what it currently is. |
| `*InputState` | The input levels as of the last pump run, and its edges. |
| `Tasks` | `ProcessEvents`, the pump, a task pinned to the main thread. |
| `Handlers` | `FlushValues`, platform's one handler, for others to order theirs around. |
| `FS`, `WriteFS` | The files: `FS` reads the overlay, and `ElevateFileAccess` gives the `WriteFS` that writes. |
| `*Values` | A key-value store of JSON values, kept in a file in `FS`. |
| `filesystems` | The writable filesystems: `NewDiskWriteFS`, `NewLocalStorageWriteFS`. |
| `headless` | Windows for tests, without an operating system: `NewWindows`. |

platform holds no platform code. The window implementation and the input
source are other plugins (gogpu's, on the
[gogpu map](https://github.com/dvoyni/cog/issues/611)). The filesystems are
adapters the application's own plugin binds.

## Quick start

A game plugin binds its files, schedules the pump on every update beat, opens
its window on the first, moves a player while W or S is held, and quits once
its window closes. gogpu's plugin, the window implementation and input
source, is not built yet: without it the pump applies nothing and reports no
input, so no window opens and none closes.

```go
package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
)

const gameName kernel.PluginName = "game"

//go:embed assets
var embedded embed.FS

// Player is the state the update track moves.
type Player struct{ X, Y float32 }

// game binds its files, opens its window, moves its player on the update
// track and quits when its window closes.
type game struct {
	supervisor *kernel.Supervisor
	writable   *filesystems.DiskWriteFS
	update     *kernel.Task[metronome.UpdateEvent, kernel.NoResult]
	close      *kernel.Task[platform.Window, kernel.NoResult]
}

func (p *game) Name() kernel.PluginName { return gameName }

// Dependencies names platform: Init reads its Tasks.
func (p *game) Dependencies() []kernel.PluginName { return []kernel.PluginName{platform.PluginName} }

func (p *game) Init(c *kernel.Configurator, _ any) error {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	p.writable, err = filesystems.NewDiskWriteFS(filepath.Join(configDirectory, "mygame"))
	if err != nil {
		return err
	}
	assets, _ := fs.Sub(embedded, "assets")
	c.BindSocket[platform.WriteFSSocket](p.writable)
	c.BindSocket(platform.ReadMountSocket{FS: assets})

	tasks, _ := c.Resource[platform.Tasks]()
	c.InitResource(Player{})
	p.update = c.RegisterTask3(update)
	p.close = c.RegisterTask1(closeWindow)
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		pump := ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}, in)
		// The update runs after the pump, so it sees this run's edges.
		return beat, ex.Schedule(p.update, beat, pump.Handle()).Handle()
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.CloseRequestedEvent) (platform.CloseRequestedEvent, kernel.JobHandle) {
		return event, ex.Schedule(p.close, event.Window, in).Handle()
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
		p.supervisor.Quit()
		return event, in
	})
	return nil
}

func (p *game) Start(*kernel.Kernel) error { return nil }
func (p *game) Stop(*kernel.Kernel) error  { return nil }
func (p *game) Destroy() error             { return p.writable.Close() }

// update opens the window on the first beat and moves the player while W or
// S is held.
func update(k *kernel.Kernel, beat metronome.UpdateEvent, manager kernel.Write[*platform.WindowManager], input kernel.Read[*platform.InputState], player kernel.Write[Player]) kernel.NoResult {
	if beat.Tick == 1 {
		manager.Get().Open(platform.WindowEntry{Title: "My game", Size: platform.Size{Width: 1280, Height: 720}})
	}
	moved := player.Get()
	if input.Get().IsHeld(platform.KeyW) {
		moved.Y -= float32(beat.Delta.Seconds()) * 100
	}
	if input.Get().IsHeld(platform.KeyS) {
		moved.Y += float32(beat.Delta.Seconds()) * 100
	}
	player.Set(moved)
	return kernel.NoResult{}
}

// closeWindow closes window, which the user asked to close.
func closeWindow(k *kernel.Kernel, window platform.Window, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
	manager.Get().Close(window)
	return kernel.NoResult{}
}

func init() { runtime.LockOSThread() }

func main() {
	s := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(60))},
		},
	}
	game := &game{supervisor: s}
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0),
		metronome.NewPlugin(),
		// gogpu.NewPlugin(), the window implementation and input source
		platform.NewPlugin(),
		game,
	); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
}
```

## Windows

A job opens, changes and closes windows on the `*WindowManager` under its
write lock. Nothing reaches the operating system until the next pump run,
which applies what changed since its last, so several changes in one frame
cost one call.

```go
window := manager.Open(platform.WindowEntry{Title: "My game", Size: platform.Size{Width: 1280, Height: 720}})
manager.SetTitle(window, "My game: level 2")
manager.SetFullscreen(window, true)
manager.Change(window, platform.WindowEntry{Title: "Map", Size: platform.Size{Width: 640, Height: 480}}) // the whole entry
manager.Close(window)
```

- **`Open` answers the `Window` at once.** The window opens on the next run,
  which publishes `WindowOpenedEvent`; from then its `Native` handle is valid,
  for gfx to make a surface from.
- **The setters** (`SetTitle`, `SetSize`, `SetFullscreen`, `SetHidden`) and
  `Change` answer false for a window that names no entry.
- **`CloseRequestedEvent` closes nothing.** The user asked; the application
  calls `Close`, or not. The window closes on the next run, which publishes
  `WindowClosedEvent`. A window the operating system closes by itself
  publishes it too. Its `Window` names nothing from then on.
- **The window state** is what a window currently is, written only by the
  pump and read by any job under the read lock: `IsOpen`, `IsFocused`,
  `Size`, `FramebufferSize`, `Scale`, `Native`, `EffectiveCursorMode`.
- **The zero `Window`** reads the only window in the window state's getters,
  and in the input state's pointer getters, so a game of one window needs no
  handle. They panic unless exactly one window is known. Everything that
  changes a window takes its real handle.

## The pump

`ProcessEvents` is a task pinned to the main thread, which platform
schedules nowhere. The application schedules it from the tracks it chooses,
from `platform.Tasks`, and **returns it in its out dependency**, so the
track's in-flight limit sees it; otherwise each beat queues another pump
behind a blocked main thread:

```go
tasks, _ := configurator.Resource[platform.Tasks]()
configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
	return beat, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
})
```

`main` keeps ticking the main thread, where the pump runs. Each run:

1. applies what changed in the window request through the `WindowsSocket`:
   opens, changes and closes windows, sets cursors, moves the cursor;
2. pumps the operating system's events, and collects the input source's
   changes;
3. folds them into the window state and the input state, under their write
   locks;
4. publishes every event in the order the changes happened.

A kernel without a window implementation or an input source pumps nothing,
so one spawned only to read files needs neither.

## Input

Input comes both as **levels**, in the `*InputState`, and as **events**.

| Reads | For |
|---|---|
| `IsHeld(key)` | Gameplay: is the key down now. |
| `JustPressed(key)`, `JustReleased(key)` | The edges of the last pump run. |
| `Label(key)` | What the layout prints on a key, for a bindings screen. |
| `PointerPosition(window)`, `IsPointerInside(window)` | Each window's pointer. |

**The edges are correct only in the update that ran the pump**: for the
jobs a handler schedules after `ProcessEvents` in the same publication, as
the quick start's update is. A job of another track, or of a later beat that
ran no pump, sees the last run's edges again, and every step of a fixed-step
catch-up sees the same ones. Such a reader folds `KeyEvent`s in its own
handler instead.

The events are, in the order the changes happened: `KeyEvent`,
`TextEvent`, `FocusEvent`, `LayoutChangedEvent`, `PointerMoveEvent`,
`ScrollEvent`, `PointerEnterEvent`, `PointerLeaveEvent`, `ResizeEvent`, and
the window's `WindowOpenedEvent`, `WindowClosedEvent` and
`CloseRequestedEvent`.

- **Keys are positions.** A `Key` names a position on a US keyboard whatever
  the layout, so `KeyW` is the key above `KeyS` on an AZERTY keyboard too, or a
  mouse button (`KeyMouseLeft`), in one space. It prints as its name, which
  `ParseKey` reads back, for saved bindings. `KeyEvent` carries the
  `Modifiers` and the key's `Label`.
- **Text is not keys.** A text field reads `TextEvent`, one per character
  typed, after the layout, dead keys and the input method. Gameplay and
  shortcuts read keys.
- **Labels follow the layout.** `Label(KeyQ)` is `'a'` on a French layout.
  The table is filled on the first run and rebuilt on `LayoutChangedEvent`.
- **Losing focus releases every held key**, each with its `KeyEvent`, so
  nothing stays held while the window is in the background.
- **Pointer moves coalesce.** A run's moves in one window are one
  `PointerMoveEvent`, at the last position, its `Delta` the moves summed.
- **Positions are in window space**, a window's logical points from its top
  left, whatever the screen's scale. The framebuffer is in pixels:
  `FramebufferSize` is `Size` times `Scale`, and `ResizeEvent` carries all
  three.
- **Scrolls** say whether `Delta` counts lines, a wheel's notches, or points,
  a touchpad's.

## The cursor

Each window's cursor is requested beside its entry, a field at a time:

```go
manager.SetCursorShape(window, platform.CursorShapePointer)
manager.SetCursorHidden(window, true)
manager.SetCursorMode(window, platform.CursorModeLocked) // or Confined, Normal
manager.SetCursorPosition(window, platform.Point{X: 640, Y: 360})
```

- **The effective mode** is the one in force, `EffectiveCursorMode(window)`.
  It reads Normal while the window lacks focus, or where the platform lacks
  the mode. The pump applies the requested mode again when the focus returns.
- **While locked**, the pointer position stays put and `PointerMoveEvent`'s
  `Delta` carries the motion, for a mouse-look camera.
- **A cursor position** moves the cursor on the next run without a pointer
  move: the user did not move it. It is dropped while the requested mode is
  Locked, and one the platform cannot set is logged.

## Files

Files are layers the application binds to platform's sockets in a plugin's
`Init`, like any binding: **at most one writable filesystem**, searched first,
and any number of **read mounts**, each at a priority. The filesystems are
adapters, not plugins, so one application mixes them freely:

```go
// on desktop
writable, err := filesystems.NewDiskWriteFS(userDirectory) // created when missing
// in a browser (js)
writable, err := filesystems.NewLocalStorageWriteFS("mygame")

c.BindSocket[platform.WriteFSSocket](writable)
c.BindSocket(platform.ReadMountSocket{FS: assets, Priority: 0}) // an embed.FS
c.BindSocket(platform.ReadMountSocket{FS: mods.FS(), Priority: 1}) // an *os.Root
```

A disk filesystem and an `os.Root` hold their directory open: the plugin that
made them closes them in its `Destroy`. [The filesystems
package](#the-filesystems-package) has what each adapter does.

platform stacks the layers into the **`FS` resource**, an alias of `fs.FS`:
the writable filesystem first, then the read mounts by priority, equal
priorities in plugin order. A listing merges every layer, a name in several
showing the highest layer's entry, so `fs.Glob` and `fs.WalkDir` work over
the overlay.

A job reads under `kernel.Read[platform.FS]`, with the `fs` helpers, and
writes under `kernel.Write[platform.FS]` through `ElevateFileAccess`. The
writes land in the writable filesystem; a reader never sees a file
mid-write:

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

- **Without a writable filesystem**, reads search the read mounts alone and
  every write returns `platform.ErrNoWriteFS`.
- **With no layer at all**, every name is missing.
- **Removing a name** removes it from the writable filesystem only; a read
  mount's file of that name is what it reads from then on.
- **Reading blocks the job's worker.** Asynchronous loading belongs to a
  later asset plugin.

## The filesystems package

`github.com/dvoyni/cog/platform/filesystems` holds the writable filesystems,
each a `platform.WriteFSSocket` the application's own plugin makes in its
`Init`, binds, and closes when it holds something open. Read mounts need no
adapter: any `fs.FS` binds as it is.

| Constructor | Where the files are | Builds | Close |
|---|---|---|---|
| `NewDiskWriteFS(directory) (*DiskWriteFS, error)` | A directory on disk. | Everywhere; a browser has no disk, so use it on desktop. | `Close`, in the binding plugin's `Destroy`. |
| `NewLocalStorageWriteFS(key) (*LocalStorageWriteFS, error)` | The page's `localStorage`, under `key`. | `js` only. | Nothing to close. |

A game picks one by build target, with a file per tag:

```go
//go:build !js

func newWriteFS() (platform.WriteFSSocket, func() error, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, err
	}
	writable, err := filesystems.NewDiskWriteFS(filepath.Join(configDirectory, "mygame"))
	if err != nil {
		return nil, nil, err
	}
	return writable, writable.Close, nil
}
```

```go
//go:build js

func newWriteFS() (platform.WriteFSSocket, func() error, error) {
	writable, err := filesystems.NewLocalStorageWriteFS("mygame")
	if err != nil {
		return nil, nil, err
	}
	return writable, func() error { return nil }, nil
}
```

### DiskWriteFS

- **The directory is created** with its parents when missing. A relative
  directory resolves against the working directory, once, when it is made.
  A directory that cannot be created or opened is the constructor's error.
- **Every name stays inside the directory**, symbolic links included: it is
  an `os.Root`. A name that climbs out, `../saves`, or a link pointing
  outside fails.
- **A name `fs.ValidPath` rejects is refused** with `fs.ErrInvalid`, such as
  `./slot`, `saves//log` or `/saves`, so nothing is written that the overlay
  could not read back.
- **It reads as fast as the disk**: `ReadFile`, `ReadDir` and `Stat` take the
  short path through the `os.Root`.
- **It holds the directory open** until `Close`; every call after it fails.
- **A directory read mount** is an `os.Root`'s `FS`, opened with
  `os.OpenRoot(directory)` and closed the same way, by the plugin that bound
  it.

### LocalStorageWriteFS

- **One JSON document under one key**, as v1 kept it: every call reads it
  whole and every change writes it whole, so what one page wrote survives a
  reload. Two filesystems on one key see each other's files.
- **It follows the disk's rules**, as path errors:
  - a file goes in a directory that exists (`fs.ErrNotExist`), never over a
    directory (`fs.ErrExist`);
  - `MkdirAll` stops at a file in the way (`fs.ErrExist`);
  - `Remove` removes a file or an empty directory (`fs.ErrExist` when it is
    not empty);
  - `Rename` moves a file, or a directory with everything in it, into a
    directory that exists, replacing a file but not a directory, and never
    into itself (`fs.ErrInvalid`);
  - an invalid name is `fs.ErrInvalid`.
- **Modes are kept**, times are not: `ModTime` is the zero time.
- **`localStorage` keeps a few megabytes per origin**, so it suits values
  and saves, not assets, which ship as an `embed.FS` read mount. A write
  over the quota fails and leaves the files as they were.
- **A key holding what is not a document** fails every call, and is never
  overwritten.
- **The constructor fails** for an empty key, or a page without
  `localStorage`.

### Testing with them

Bind them as a game does, from a test plugin in a kernel of platform, and
assert through the `FS` resource and the files themselves: the disk one
against `t.TempDir()`, the `localStorage` one under Node, which keeps
`localStorage` in a file given on its command line:

```
GOOS=js GOARCH=wasm NODE_OPTIONS=--localstorage-file=/tmp/localstorage \
	go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./filesystems/
```

## Values

`*Values` keeps a key-value store of JSON values in a file in `FS`,
`config.json` unless `Config.ValuesFile` names another. `Get` takes the `FS`,
under its read lock; `Set`, `Delete` and `Flush` take the `WriteFS`, under its
write lock:

```go
read := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Read[platform.FS], values kernel.Read[*platform.Values]) kernel.NoResult {
	volume, found, err := values.Get().Get(filesystem.Get(), "volume")
	// ...
	return kernel.NoResult{}
})
write := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Write[platform.FS], values kernel.Write[*platform.Values]) kernel.NoResult {
	err := values.Get().Set(platform.ElevateFileAccess(filesystem), "volume", json.RawMessage(`0.5`))
	// ...
	return kernel.NoResult{}
})
```

- **Read once, through the overlay.** The first call reads the file, so a
  read mount may ship the defaults, and keeps it. A missing file holds no
  values; a corrupt one is an error from every call. A value that is not JSON
  is refused.
- **Written on a flush.** A change stays in the cache until `Flush`, which
  writes the file to the writable filesystem when something changed.
  platform flushes on every `WindowClosedEvent` too. A plugin listed after
  platform orders its own `WindowClosedEvent` handler around that flush
  with `OrderSubscriptions(mine, handlers.FlushValues)`, `handlers` read
  with `configurator.Resource[platform.Handlers]()`, so a value it sets on
  the close is flushed with it.
- **Config before `Spawn`.** A kernel's config cannot change once spawned, so
  config kept in the values is read first. `main` spawns a throwaway kernel
  of platform and a plugin binding the filesystems, with no window
  implementation, reads the values in a job, destroys it, and spawns the real
  kernel with that config.

## Implementing the window and input seams

A plugin implementing windows binds a `WindowsSocket`, and one implementing
input an `InputSourceSocket`, in its `Init`, anywhere in `Spawn`. Only the
pump calls them, on the main thread:

- **`Open`, `Change`, `Close`** apply the window request. `Open` reports the
  window open with an `InputChangeWindowOpened` from a later `Pump`, carrying
  its sizes, scale and `NativeHandle`. `Change` gets the entry as last
  applied and as requested.
- **`SetCursor`, `EffectiveCursorMode`, `SetCursorPosition`** apply the
  cursor request and answer the mode in force, which the pump asks after
  every `Pump`.
- **`Pump`** handles the operating system's events and appends the window
  changes they make, as `InputChange`s.
- **`AppendChanges`** appends the input source's changes, in order, and
  **`Label`** answers what the current layout prints on a key.

An `InputChange` is pointer-free and names a window the window
implementation was asked to open and has not reported closed; any other, or
a key change naming no `Key`, panics. platform folds the changes into the
state and the events, so an implementation reports raw changes and owns no
logic of its own. A test binds a fake of both seams and feeds the changes for
each run, asserting only through the `InputState`, the `WindowManager` and
the events, as platform's own tests do.

## Rules

- **The application schedules the pump**, and returns it in the out
  dependency.
- **Nothing reaches the operating system but in the pump.** A job changes the
  window request; the next run applies it.
- **A close request closes nothing.** The application calls `Close`.
- **Filesystems are adapters.** The application's plugin makes them, binds
  them and closes them; platform's `Config` names no path.
- **Writes take the write lock.** Only `ElevateFileAccess` gives a `WriteFS`,
  from `kernel.Write[platform.FS]`.
- **Composition errors panic** at `Spawn`: two bindings of an exclusive
  socket, a read mount without an `FS`, a config of another type than
  `Config`.
