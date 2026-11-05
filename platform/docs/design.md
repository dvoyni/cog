# platform: design

`github.com/dvoyni/cog/platform` is what the operating system provides to a
kernel: **windows**, **input** and **filesystems**. It declares them as
interfaces, and implementing plugins fill them: the gogpu plugin implements
windows and input, and the `disk` plugin implements the filesystems and the
values. platform itself holds no platform code. It owns the logic that is the
same for every implementation: the window request and window state, folding
input into state and events, and the overlay of filesystems.

Status: designed, not built. The decisions come from the
[platform map](https://github.com/dvoyni/cog/issues/610); each section links
the ticket that holds its detail. The vocabulary is in
[`CONTEXT.md`](../CONTEXT.md).

## Constraints

- **No platform code.** platform imports the kernel and the standard library
  only. It has no build tags, no cgo, and does no file or OS I/O of its own.
- **Pointer-free work.** Events, task args and results carry no pointers or
  strings, as the kernel requires. Titles, paths and text live in resources.
- **Main thread for the OS.** Every window, cursor and event call reaches the
  operating system from one pinned task, on every platform
  ([What must run on the main thread on each platform](https://github.com/dvoyni/cog/issues/645)).
  Only file I/O is free of that.
- **No ECS import, no metronome import.** platform subscribes to nothing; the
  application wires its pump into whichever tracks it wants.

## Shape

| Type | Kind | What it is |
|---|---|---|
| `Windows` | interface, resource | The window seam: opens, changes and closes OS windows and pumps their events. Installed by exactly one plugin (gogpu). |
| `InputSource` | interface, resource | The input seam: reports the input changes since the last pump run. Installed by exactly one plugin (gogpu, later a scripted one). |
| `Window` | handle | A pointer-free generational handle naming one window. |
| `WindowRequest` | resource | What the application wants its windows to be. |
| `WindowState` | resource | What each window currently is. |
| `InputState` | resource | Input levels as of the last pump run. |
| `ProcessEvents` | pinned task | The pump. |
| `FileSystem`, `WritableFileSystem` | interfaces | Read and write access to files. |
| `ReadMounts` | resource | The read mounts plugins contribute. |
| `Files` | resource | The overlay, read as a `FileSystem`, written as a `WritableFileSystem`. |
| `Values` | interface, resource | The key-value store, installed by the filesystem implementation. |

**Finding none or two of a seam panics** as a composition error: `Windows`,
`InputSource`, the writable filesystem and `Values` each need exactly one
installer. An input source naming a window the `Windows` implementation does
not know panics too
([Scripted input for tests and agents](https://github.com/dvoyni/cog/issues/644)).

Implementing plugins are listed before platform in `Spawn`, install their
implementations in `Init`, and platform's `Init` finds them, as gfx finds its
backend.

## Windows

From [Several windows, and which window an event belongs to](https://github.com/dvoyni/cog/issues/643).

- **Several windows from the start.** `Window` is a generational handle like
  gfx's resources: zero means none, and a handle kept past its window's close
  names nothing rather than a later window.
- **The main window** is the one the implementing plugin opens at start, from
  its own `Config`.
- **The window request** is one resource holding an entry per desired window:
  title, size, fullscreen, visibility, and its cursor request. A job writes it
  under the write lock:
  - **open:** add an entry and get its `Window` at once; the OS window is
    created on the next pump run;
  - **change:** edit the entry;
  - **close:** remove the entry.

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
- **A handler that schedules it returns it in the out dependency**
  (`ex.Join(in, processEvents)`), so the track's in-flight limit sees it.
  Otherwise each beat queues another pump behind a blocked main thread.
- **One run:**
  1. apply the window request's changes through `Windows`;
  2. let `Windows` pump the OS's pending events;
  3. collect the batch of input changes from `InputSource`;
  4. fold them into `WindowState` and `InputState` under their write locks;
  5. publish the events, in the order the changes arrived.

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

- **Both.** `InputState` holds levels; input events carry every change in
  order. Gameplay polls; UI and text read events.
- **No edges.** The state has no "just pressed". A game that wants one
  compares levels or folds events in its own handler, scheduled after
  `ProcessEvents`; with several tracks and fixed-step catch-up, any edge kept
  by platform would be seen twice by one reader or missed by another.
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
- **Modifiers:** Shift, Control, Alt, Super, and the Caps Lock and Num Lock
  states, as a bitmask. Left and right modifier keys are separate `Key`s;
  AltGr is `KeyRightAlt` with no Control.
- **Focus loss releases every held key** and publishes the releases, so no
  key stays stuck after Alt-Tab.

### Text

- **Text is not keys.** `TextEvent{Window, Char}` carries one committed
  character, after the layout, dead keys and the IME, in order with the key
  events. Shift, AltGr, dead keys, IME commits, the emoji picker and
  dictation all type text no key's label matches, or with no key at all. The
  input state holds no text.
- **Committed text only.** IME composition is out of scope (below).

### Events

Every event names its `Window`. Key and text events name the focused window;
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
- **A later scripted input source** installs only `InputSource`, beside
  gogpu's windows (an agent driving a running game) or a headless window
  implementation.

## Cursor

From [Cursor control: show, hide, lock and shape](https://github.com/dvoyni/cog/issues/642).

- **The cursor request is part of each window's entry** in the window
  request: shape, visibility, mode, and a pending warp. The pump applies what
  changed.
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
- **Warp.** `Warp(position)` sets a pending target in window space; the pump
  moves the OS cursor there on its next run and clears it, and several warps
  before one run collapse to the last. A warp produces no `PointerMoveEvent`,
  and the input state's position becomes the target. A warp while locked is
  ignored.

## Filesystems

From [Filesystems before Spawn and as a resource](https://github.com/dvoyni/cog/issues/639).

### Interfaces

- **`fs.FS` ← `FileSystem` ← `WritableFileSystem`.**
  - `FileSystem` is `fs.FS` + `fs.ReadDirFS` + `fs.StatFS` + `fs.ReadFileFS`.
    A read mount implements it, and so does the overlay.
  - `WritableFileSystem` adds `WriteFile`, `MkdirAll`, `Remove`, `Rename` and
    `AppendFile`, all required.
  - A platform helper wraps a plain `fs.FS`, such as an `embed.FS`, into a
    `FileSystem`.

### The overlay

- **Implementing plugins contribute layers in `Init`.** Any plugin appends
  read mounts, each with a priority, to `ReadMounts`: the disk plugin mounts
  the directories its `Config` names, a game's own plugin mounts its
  `embed.FS`. Exactly one plugin installs the writable filesystem.
- **Where the files are is the implementation's `Config`,** since every
  implementation takes different parameters. platform's own `Config` names no
  paths.
- **platform stacks them into `Files`.** The writable filesystem is searched
  first, then the read mounts by priority, equal priorities in registration
  order.
- **Listings merge.** `ReadDir` returns the union of every layer's names; a
  name in several layers shows the higher layer's entry, file or folder. So
  `Open` and listings agree, and `fs.Glob` and `fs.WalkDir` work over the
  overlay.
- **The read handle gives a `FileSystem`, the write handle a
  `WritableFileSystem`** whose writes land in the writable layer. No reader
  sees a file mid-write.
- **Several kernels** each have their own `Files`, and nothing is locked
  across kernels. Two kernels writing the same files are kept apart by the
  application (different folders) or by the implementation.
- **Blocking I/O.** A job reading a file blocks its worker. Asynchronous
  loading belongs to a later asset plugin.

### Values

- **The key-value store carries over from v1.** platform declares `Values`;
  the filesystem implementation installs it with `InitResource[platform.Values]`.
  Get works under the read handle; set, delete and flush need the write
  handle, and set and delete also come without a flush.
- **The only thing usable before `Spawn`.** A kernel's config is given to
  `Spawn` and plugins cannot change it, so config kept in a stored file is
  read in `main`. Each implementing package exports

  ```go
  func ReadValues(config Config, keys []string, unmarshal bool) ([]any, error)
  ```

  taking the same `Config` as its plugin, so both find the same file. It
  returns one value per key, `nil` for a missing key: decoded JSON when
  `unmarshal` is true, `json.RawMessage` when false. A missing file means a
  first launch: all `nil`, no error. A corrupt file or a failing backend is an
  error.
- **Read-only before `Spawn`.** `main` reads, then passes the same `Config`
  to `Spawn`; inside the kernel the values exist only as the resource.

## The disk plugin

`github.com/dvoyni/cog/disk` implements the filesystems for desktop
(`!js`): read mounts from the directories its `Config` names, the writable
filesystem in a directory its `Config` names, the values file in it, and
`ReadValues`. A web implementation over browser storage comes later, behind
`js`.

## The gogpu fork

Three fork changes, each its own build ticket proposed upstream as a PR:

- [Positional keys on Windows in the gogpu fork](https://github.com/dvoyni/cog/issues/646):
  Windows maps keys from the layout-dependent virtual-key code; every other
  platform already reports positions.
- [Cursor lock and confine on macOS in the gogpu fork](https://github.com/dvoyni/cog/issues/647):
  macOS's cursor mode is a no-op today, so its effective mode reads Normal.
- [Cursor warp in the gogpu fork](https://github.com/dvoyni/cog/issues/648):
  gogpu has no warp.

gogpu's implementation of `Windows` and `InputSource` is built on the
[gogpu](https://github.com/dvoyni/cog/issues/611) map.

## Testing

Two seams, both through the interfaces an implementation installs:

- **Windows and input.** A kernel spawned with platform and a fake that
  installs `Windows` and `InputSource` and feeds input changes per pump run.
  Tests assert only through `InputState`, `WindowState` and the published
  events: window open and close, coalescing, focus loss, locked deltas, key
  labels and layout changes, warps, window-space coordinates. gogpu's
  implementation reuses the same seam.
- **Filesystems.** The overlay over in-memory read mounts and an in-memory
  writable filesystem, checked with `fstest.TestFS` and with tests of merged
  listings, priority and writes. The disk plugin passes the same tests against
  a temporary directory, and `ReadValues` is tested on a values file the
  plugin wrote, including the missing and corrupt cases.

## Out of scope

- **IME composition:** preedit text, its cursor and the candidate window's
  position. Committed text works; composition returns as its own effort once
  the fork implements it, beside `TextEvent`, which does not change.
- **Scripted input and a headless window implementation:** later plugins on
  the input seam, an MCP-driven input source among them.
- **Custom image cursors:** a game hides the OS cursor and draws its own.
- **Gamepads and the clipboard:** later.
- **Audio, and v1's platform slot** (Steam, achievements). If the storefront
  returns, it needs a name other than platform.
