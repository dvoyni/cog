# platform: design

`github.com/dvoyni/cog/platform` is what the operating system provides to a
kernel: **windows**, **input** and **filesystems**. It declares them as
interfaces, and implementing plugins fill them: the gogpu plugin implements
windows and input, and the application's own plugin binds the filesystems,
through the adapters of `platform/filesystems`. platform itself holds no
platform code. It owns the logic that is the same
for every implementation: the window request and window state, folding input
into state and events, the overlay of filesystems, and the values kept in it.

Status: windows, the pump, the filesystems, keys, text, focus, the pointer,
scroll, resize, key labels, the cursor, the filesystem adapters and the
headless windows adapter are built. The decisions come from the
[platform map](https://github.com/dvoyni/cog/issues/610); each section links
the ticket that holds its detail. The vocabulary is in
[`CONTEXT.md`](../CONTEXT.md).

## Constraints

- **No platform code.** platform imports the kernel and the standard library
  only. It has no build tags, no cgo, and does no file or OS I/O of its own;
  its `filesystems` subpackage holds the adapters that do.
- **Pointer-free work.** Events, task args and results carry no pointers or
  strings, as the kernel requires. Titles, paths and text live in resources.
- **Main thread for the OS.** Every window, cursor and event call reaches the
  operating system from one pinned task, on every platform
  ([What must run on the main thread on each platform](https://github.com/dvoyni/cog/issues/645)).
  Only file I/O is free of that.
- **No ECS import, no metronome import.** platform subscribes only to its own
  `WindowClosedEvent`, to flush the values; the application wires its pump
  into whichever tracks it wants.

## Shape

| Type | Kind | What it is |
|---|---|---|
| `WindowsSocket` | optional exclusive socket | The window seam: opens, changes and closes OS windows and pumps their events. Bound by gogpu, or in a test by `headless.NewWindows`. |
| `InputSourceSocket` | optional exclusive socket | The input seam: reports the input changes since the last pump run. Bound by gogpu, later a scripted plugin. |
| `Window` | handle | An opaque, pointer-free handle naming one window, from a counter that is never reused. |
| `WindowManager` | resource | Every window: its entry in the window request, what the application wants it to be, and its window state, what it currently is. |
| `InputState` | resource | Input levels as of the last pump run. |
| `Tasks` | resource | Every task platform offers others to schedule: `ProcessEvents`. |
| `Handlers` | resource | Every handler platform subscribes, `FlushValues`, for others to order theirs around with `OrderSubscriptions`. |
| `ProcessEvents` | pinned task | The pump. |
| `FS` | alias of `fs.FS`, resource | Read access to files: the overlay, which only reads. |
| `WriteFS`, `ElevateFileAccess` | interface, function | Write access to files: the overlay that writes, which `ElevateFileAccess` gives from the write handle on `FS` alone. |
| `WriteFSSocket` | optional exclusive socket | The writable filesystem the application binds: `filesystems.NewDiskWriteFS` or `filesystems.NewLocalStorageWriteFS`. |
| `ReadMountSocket` | optional socket | A read mount the application binds: any `fs.FS` at a priority. |
| `Values` | resource | The values, a JSON file in `FS`: `Get` takes the `FS`; `Set`, `Delete` and `Flush` take the `WriteFS`. |

**A seam bound wrongly panics** as a composition error, which the kernel
reports at `Spawn`: two bindings of an exclusive seam. Every seam is
optional: a kernel without the window and input seams, such as one spawned
only to read the values, applies no window request and reports no input, and
one without filesystems finds no file and writes none. An input source naming a window the window implementation
does not know panics too
([Scripted input for tests and agents](https://github.com/dvoyni/cog/issues/644)).

Every seam is a socket, so implementing plugins are listed anywhere in
`Spawn`.

## Windows

From [Several windows, and which window an event belongs to](https://github.com/dvoyni/cog/issues/643).

- **Several windows from the start.** `Window` is an opaque handle from a
  counter that is never reused, as `KernelID` is: zero means none, and a
  handle kept past its window's close names nothing rather than a later
  window.
- **The zero `Window` reads the only window.** The getters of the window
  state and the input state, given the zero `Window`, answer for the one
  window the window state knows, and panic unless it knows exactly one. A
  game of one window reads them without a handle. Edits and input changes
  still name their window.
- **The main window** is the one the implementing plugin opens at start, from
  its own `Config`.
- **One resource, the `WindowManager`,** holds both the window request and
  the window state. A job writes it under the write lock; the window state
  in it is written only by the pump, and read by any job under the read lock.
- **The window request** holds, per desired window, its entry, title, size,
  fullscreen and visibility, and beside it the window's cursor request.
  - **open:** `Open` adds an entry and answers its `Window` at once; the OS
    window is created on the next pump run;
  - **change:** `Change` replaces the whole entry, and `SetTitle`, `SetSize`,
    `SetFullscreen` and `SetHidden` set one field;
  - **close:** `Close` removes the entry.

  The pump applies only what changed, so repeated writes in one frame cost
  one OS call. Titles live in the resource.
- **The window state**, per window: open or not yet, window size, framebuffer
  size, scale, focus, effective cursor mode, and the native handle gfx creates
  its `Surface` from.
- **Lifecycle events:** `WindowOpenedEvent` once the OS window exists and its
  native handle is valid, `WindowClosedEvent` once it is gone, including a
  window the OS closed by itself. gfx creates and releases surfaces on them.
- **Closing is the application's.** `CloseRequestedEvent` only asks; nothing
  closes until the entry is removed. Quitting is the supervisor's
  ([Quitting](https://github.com/dvoyni/cog/issues/633)), and gogpu's
  quit-on-last-window-closed is turned off.

## The pump

From [The os pump and the tracks](https://github.com/dvoyni/cog/issues/632).

- **platform registers `ProcessEvents`, pinned to the main thread, and
  schedules it nowhere.** The application schedules it from the track handlers
  it chooses: an update track, a render track, or both.
- **A plain task function over a private resource.** What the pump keeps
  between runs, the windows as last applied and the batch's buffer, is a
  resource of an unexported type, so no job but `ProcessEvents` locks it.
- **A handler that schedules it returns it in the out dependency**
  (`ex.Join(in, processEvents)`), so the track's in-flight limit sees it.
  Otherwise each beat queues another pump behind a blocked main thread.
- **One run:**
  1. apply the window request's changes through the `WindowsSocket`;
  2. let it pump the OS's pending events;
  3. collect the batch of input changes from the `InputSourceSocket`;
  4. fold them into the `WindowManager` and `InputState` under their write locks;
  5. publish the events, in the order the changes arrived.

  Without a window implementation a run applies and pumps nothing, and
  without an input source it collects no input.

  Handlers only schedule jobs, and those jobs run after the locks are
  released, so they see the state as of the end of the run.
- **A Windows modal drag holds the pump**, and so the beat whose out
  dependency includes it: the track owes steps and catches up after the drag.
  No special case.
- **A beat that ran no pump** sees no new input: the state is unchanged.

## Input

From [Input: events, a state resource, or both, and the event types](https://github.com/dvoyni/cog/issues/640)
and [Key codes and text input: physical keys, the layout and the IME](https://github.com/dvoyni/cog/issues/641).

### State and events

- **Both.** `InputState` holds levels and the last run's edges; input events
  carry every change in order. Gameplay polls; UI and text read events.
- **Edges of the last run.** `JustPressed` and `JustReleased` say what went
  down and up during the last pump run, so a tap pressed and released in one
  run shows both. A repeat is no press; a release on focus loss is a
  release. They are correct only for the jobs scheduled after
  `ProcessEvents` in the same publication, the update that processed the
  events: with several tracks and fixed-step catch-up, a job of another
  track or of a beat that ran no pump sees the last run's edges again. Such
  a reader folds `KeyEvent`s in its own handler instead.
- **What is global and what is per window:** held keys and key labels are
  global, since there is one keyboard; the pointer position and whether it is
  inside are per window.

### Keys

- **A `Key` names a physical position,** with US-layout names, as web `code`
  and SDL scancodes do: `KeyW` is above `KeyS` on every keyboard. Keyboard
  keys and mouse buttons share the one `Key` space, so a binding is a single
  lookup.
- **A `Key` prints as its name and parses back** (`"KeyW"`), so bindings in
  config files and values are readable.
- **Key labels.** `InputState.Label(key) rune` gives the character the current
  layout prints on a key, unshifted, or 0. The pump fills the table at start
  and rebuilds it when the layout changes, since the OS layout APIs are bound
  to a thread; any job reads it under the read lock. `KeyEvent.Label` carries
  the same value, so a handler needs no lock.
- **The input source labels one key at a time,** as every OS layout API
  does: `InputSourceSocket.Label(key)`. The pump asks it for every keyboard
  key on its first run and at each layout change, which the source reports
  as an input change naming no window, since the keyboard is global. Mouse
  buttons are never asked and label 0. A key event takes its label at its
  place in the run: before a layout change the old one, after it the new.
- **Modifiers:** Shift, Control, Alt, Super, and the Caps Lock and Num Lock
  states, as a bitmask. Left and right modifier keys are separate `Key`s;
  AltGr is `KeyRightAlt` with no Control.
- **Focus loss releases every held key** and publishes the releases, so no
  key stays stuck after Alt-Tab. They come before the `FocusEvent`, in `Key`
  order, carrying only the lock states for modifiers. A release of a key not
  held, such as the operating system's for one released that way, is
  dropped, so every published release follows a published press. A mouse
  button's release carries the window's pointer position.

### Text

- **Text is not keys.** `TextEvent{Window, Char}` carries one committed
  character, after the layout, dead keys and the IME, in order with the key
  events. Shift, AltGr, dead keys, IME commits, the emoji picker and
  dictation all type text no key's label matches, or with no key at all. The
  input state holds no text.
- **Committed text only.** IME composition is out of scope (below).

### Pointer

- **Each window keeps its pointer:** its last position, and whether the
  pointer is inside. Entering sets both, from the position the enter
  carries; leaving keeps the position. A closed window's pointer is
  forgotten.
- **Moves coalesce.** A run's moves in one window become one
  `PointerMoveEvent`, in the place of the last, with the final position and
  the motions summed. platform takes each motion from the positions, from
  the last one the window saw; the first move in a window the pointer did
  not enter has none.
- **The window named** is the one under the pointer, or the one that
  captured it during a drag, which the source decides; a captured position
  may lie outside the window.
- **Scroll** carries its amounts and whether they count lines, as a wheel's
  notches, or window-space points, as a touchpad's. Scrolls do not
  coalesce.
- **A resize** sets the window state's size, framebuffer size and scale
  before its `ResizeEvent` is published.

### Events

Every event names its `Window`, but `LayoutChangedEvent`, since the keyboard
is global. Key and text events name the focused window;
pointer events name the window under the pointer, or the captured one during
a drag.

| Event | Carries |
|---|---|
| `KeyEvent` | key, pressed, repeat, modifiers, label; the pointer position for mouse buttons |
| `TextEvent` | one committed character |
| `PointerMoveEvent` | final position and summed delta; at most one per pump run per window |
| `ScrollEvent` | dx, dy, and whether in lines or pixels |
| `PointerEnterEvent`, `PointerLeaveEvent` | |
| `FocusEvent` | gained or lost |
| `ResizeEvent` | window size, framebuffer size, scale |
| `LayoutChangedEvent` | the keyboard layout changed; key labels were rebuilt |
| `CloseRequestedEvent` | the user asked to close the window |
| `WindowOpenedEvent`, `WindowClosedEvent` | see Windows |

### Coordinates

- **Window space.** Every pointer position is in a window's logical points,
  as `float32`, matching gogpu's pointer events on every platform.
- **Framebuffer space** is window space times the window's scale; the window
  state holds both sizes and the scale. Picking against a render target
  converts at that one point.

### Folding

From [Scripted input for tests and agents](https://github.com/dvoyni/cog/issues/644).

- **platform folds; sources only report.** An input source hands platform a
  batch of pointer-free **input changes** per pump run. platform applies the
  rules above once, for every source: coalescing pointer moves, releasing keys
  on focus loss, state before events, deltas while locked, key labels.
- **A later scripted input source** binds only `InputSourceSocket`, beside
  gogpu's windows (an agent driving a running game) or the headless windows
  adapter.

## Cursor

From [Cursor control: show, hide, lock and shape](https://github.com/dvoyni/cog/issues/642).

- **The cursor request is its own, beside each window's entry** in the
  window request: shape, visibility, mode, and a pending cursor position.
  `SetCursorShape`, `SetCursorHidden`, `SetCursorMode` and
  `SetCursorPosition` set one each; nothing reads it back, and `Entry` and
  `Change` do not carry it. The pump applies what changed.
- **Visibility is a flag of its own,** so hiding does not forget the shape.
- **Shapes:** default, pointer, text, crosshair, move, the four resizes,
  not-allowed, wait.
- **Modes:** Normal; Confined (visible, kept in the window); Locked (hidden
  and confined). While locked, `PointerMoveEvent`'s position stays frozen and
  its delta carries the relative motion.
- **Focus.** The OS drops a lock or confine on focus loss; the pump re-applies
  the requested mode when focus returns. The window state holds the
  **effective mode**, so a game sees when the lock is not in force, on focus
  loss or where a platform lacks the mode.
- **Cursor position.** `SetCursorPosition(position)` sets a pending cursor
  position in window space; the pump moves the OS cursor there on its next
  run and clears it, and several set before one run collapse to the last.
  Setting it produces no `PointerMoveEvent`, and the input state's pointer
  position becomes it. Setting it while locked is ignored: the run drops it
  when the requested mode is Locked as it applies the request.
- **The seam.** The `WindowsSocket` applies the cursor request with
  `SetCursor(window, CursorRequest)`, apart from `Open` and `Change`: after
  `Open` when it is not the zero request, when it changes, and on focus
  return. The pump asks
  `EffectiveCursorMode(window)` of every window after each `Pump`, and
  re-applies the cursor on focus return only when the effective mode is not
  the requested one. `SetCursorPosition(window, position)` moves the cursor;
  an error is logged and leaves the pointer position as it was. While locked,
  the source reports the mouse's motion as a pointer move's `PointerDelta`.

## Filesystems

From [Filesystems before Spawn and as a resource](https://github.com/dvoyni/cog/issues/639).

### Interfaces

- **`FS` ← `WriteFS`.** platform declares only the writing interface.
  - The resource is `FS`, an alias of the standard `fs.FS`, read with `fs.ReadFile`,
    `fs.ReadDir`, `fs.Stat`, `fs.Glob` and `fs.WalkDir`. The overlay
    implements `ReadDirFS`, `StatFS` and `ReadFileFS`, so the helpers take
    the short path.
  - A read mount is any `fs.FS`: an `embed.FS` or `os.DirFS` binds as it
    is, and the overlay reads each layer through the same helpers.
  - `WriteFS` adds `WriteFile`, `MkdirAll`, `Remove`, `Rename` and
    `AppendFile`, all required. `WriteFSSocket`, the writable filesystem's
    socket, is one.

### The overlay

- **The application's plugins bind layers to platform's sockets in `Init`,**
  listed anywhere in `Spawn`. Filesystems are adapters, not plugins, so one
  application mixes them freely. Any plugin binds read mounts,
  `BindSocket(platform.ReadMountSocket{FS, Priority})`: an `embed.FS`, an
  `os.Root`'s `FS`. At most one binds the writable filesystem,
  `BindSocket[platform.WriteFSSocket](writable)`; two is a composition error
  the kernel reports. Without one, the `FS` resource reads the read mounts
  alone and every write fails with `ErrNoWriteFS`; without any layer, every
  name is missing. Sockets are no resources, so no job reaches a
  layer but through the `FS` resource, which holds the socket handles and
  orders the layers on first use. A read mount without an `FS` panics in
  platform's `Start`.
- **Where the files are is the binding plugin's choice,** given to the
  adapters it makes. platform's own `Config` names no paths.
- **platform stacks them into the `FS` resource.** The writable
  filesystem is searched first, then the read mounts by priority, equal priorities in plugin
  order.
- **Listings merge.** `ReadDir` returns the union of every layer's names; a
  name in several layers shows the higher layer's entry, file or folder. So
  `Open` and listings agree, and `fs.Glob` and `fs.WalkDir` work over the
  overlay.
- **The `FS` resource is read under a read lock and written under a
  write lock**, the writes landing in the writable layer. The resource only
  reads, and its value cannot be asserted into writing;
  `ElevateFileAccess(kernel.Write[FS])` gives the `WriteFS` that writes,
  so a reader cannot write. No reader sees a file mid-write.
- **Several kernels** each have their own `FS` resource, and nothing is locked
  across kernels. Two kernels writing the same files are kept apart by the
  application (different folders) or by the implementation.
- **Blocking I/O.** A job reading a file blocks its worker. Asynchronous
  loading belongs to a later asset plugin.

### Values

- **The key-value store carries over from v1,** its values JSON, kept in a
  file in `FS`: `Config.ValuesFile`, `config.json` when empty.
- **A resource whose methods take the filesystem.** `*Values` keeps the file
  name and the cache. `Get` takes the `FS`, so it works under the read lock;
  `Set`, `Delete` and `Flush` take the `WriteFS`, so they need the write
  lock. The lock on `FS` guards the values, so no reader sees the file
  mid-write.
- **Read once, in place.** The first call reads the file through the
  overlay, so a read mount may ship the defaults, and keeps it in the `FS`
  resource. A missing file holds no values; a corrupt one is an error from
  every call. A value that is not JSON is refused.
- **Written on a flush.** A change stays in the cache until `Flush`,
  which writes the file to the writable layer when something changed.
  platform flushes on every `WindowClosedEvent`, from a task holding the
  `FS` write lock, after the handlers before it. Its subscription is
  `Handlers.FlushValues`, so a plugin listed after platform orders its own
  close handler before or after the flush.
- **Before `Spawn`.** A kernel's config is given to `Spawn` and plugins
  cannot change it, so config kept in the values file is read first: `main`
  spawns a throwaway kernel of platform and a plugin binding the filesystems,
  starts it, reads the values with `Values.Get`, and destroys it, then spawns
  the real kernel with that config. platform spawns without a window or
  input implementation for this.

## The filesystem adapters

From [disk: read-mount directories and the writable directory](https://github.com/dvoyni/cog/issues/654).
`github.com/dvoyni/cog/platform/filesystems` holds the writable
filesystems, each a constructor the binding plugin calls in its `Init`.
Read mounts need none: any `fs.FS` binds as it is.

- **`NewDiskWriteFS(directory)`** keeps the files in a directory on disk,
  created with its parents when missing; a relative directory resolves
  against the working directory, once. Every name stays inside it, symbolic
  links included: it is an `os.Root`, open until `Close`, which the binding
  plugin calls in its `Destroy`. A write refuses a name `fs.ValidPath`
  rejects, so nothing is written that the overlay could not read back. A
  directory read mount is an `os.Root`'s `FS`, opened and closed the same
  way.
- **`NewLocalStorageWriteFS(key)`** (`js`) keeps the files in the page's
  `localStorage`, as v1 did: one JSON document under the key, read whole by
  every call and written whole by every change, so it survives a reload. It
  follows the disk's rules: a file's directory must exist, nothing is written
  over a directory, and a directory is removed only empty. `localStorage`
  keeps a few megabytes per origin, so it suits values and saves; a write
  over the quota fails and leaves the files as they were.

## The headless windows adapter

From [The headless recording adapter](https://github.com/dvoyni/cog/issues/670),
built on the [gfx map](https://github.com/dvoyni/cog/issues/608).
`github.com/dvoyni/cog/platform/headless` holds windows without an operating
system, for tests: **`NewWindows()`** answers a `WindowsSocket` the test's
own plugin binds, an adapter, not a plugin. A window opens from the next pump
at its requested size, at scale 1, with the zero native handle, from which
gfx's gogpu backend makes a headless surface; closing it reports it gone. It
has no focus, cursor or input.

## The gogpu fork

Three fork changes, each its own build ticket proposed upstream as a PR:

- [Positional keys on Windows in the gogpu fork](https://github.com/dvoyni/cog/issues/646):
  Windows maps keys from the layout-dependent virtual-key code; every other
  platform already reports positions.
- [Cursor lock and confine on macOS in the gogpu fork](https://github.com/dvoyni/cog/issues/647):
  done in the fork and proposed as [gogpu#519](https://github.com/gogpu/gogpu/pull/519).
  Both modes detach the cursor from the mouse; Confined moves the visible
  cursor itself, clamped to the content area. While the window is unfocused
  the effective mode reads Normal.
- [Cursor position in the gogpu fork](https://github.com/dvoyni/cog/issues/648):
  done in the fork as `App.SetCursorPosition` and proposed as
  [gogpu#521](https://github.com/gogpu/gogpu/pull/521), stacked on gogpu#519.
  Wayland supports it only while locked, through the locked pointer's position
  hint; elsewhere it reports unsupported.

gogpu's implementation of `WindowsSocket` and `InputSourceSocket` is built on the
[gogpu](https://github.com/dvoyni/cog/issues/611) map.

## Testing

Two seams, both through the interfaces an implementation installs:

- **Windows and input.** A kernel spawned with platform and a fake that
  binds `WindowsSocket` and `InputSourceSocket` and feeds input changes per
  pump run.
  Tests assert only through `InputState`, the `WindowManager` and the
  published events: window open and close, coalescing, focus loss, locked deltas, key
  labels and layout changes, cursor positions, window-space coordinates. gogpu's
  implementation reuses the same seam.
- **Filesystems.** The overlay over in-memory read mounts and an in-memory
  writable filesystem, checked with `fstest.TestFS` and with tests of merged
  listings, priority and writes; values read once through the overlay,
  flushed to the writable layer, and on a window's close, with the missing
  and corrupt cases, and with no layer bound. Each adapter passes them
  bound by a test plugin in a kernel of platform: the disk one against
  temporary directories, the `localStorage` one under Node
  (`GOOS=js GOARCH=wasm`, `NODE_OPTIONS=--localstorage-file=...`).

## Out of scope

- **IME composition:** preedit text, its cursor and the candidate window's
  position. Committed text works; composition returns as its own effort once
  the fork implements it, beside `TextEvent`, which does not change.
- **Scripted input:** a later plugin on the input seam, an MCP-driven input
  source among them.
- **Custom image cursors:** a game hides the OS cursor and draws its own.
- **Gamepads and the clipboard:** later.
- **Audio, and v1's platform slot** (Steam, achievements). If the storefront
  returns, it needs a name other than platform.
