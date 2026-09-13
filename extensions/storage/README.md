# storage

`github.com/dvoyni/cog/extensions/storage` provides one kernel resource,
`FileSystem`: a prioritized read-only overlay over every mounted filesystem,
including the single permanent one that writes land in.

storage is a **Port**: it ships its own contract and implementation, and works
only once a `PermanentFS` **Adapter** is bound to it. The vocabulary is in
[`CONTEXT.md`](../../CONTEXT.md) and the decision in
[ADR 0001](../../docs/adr/0001-bundles-slots-ports-and-adapters.md).

## Packages

storage has the Port shape: a contract root, an `…impl` and an `internal/`.

- **`extensions/storage`** is the contract: the commands, `FileSystem`,
  `WriteFS`, `WriteAccess`, `Values`, `ReadMount`, `PermanentFS`, the errors and
  `Name`. It declares no plugin, and it is what every other package imports.
- **`extensions/storage/storageimpl`** is the plugin: `New`, `Config` and the
  command handlers. Only composition roots and tests import it.
- **`extensions/storage/internal`** declares `FileSystem`, `WriteFS`, `Values`
  and the value requests, whose unexported state the handlers read, and the
  root aliases them.

The Adapters are separate plugins in `extensions/`:

- **`extensions/diskfs`** (`!js`): a directory under the user's data directory,
  named by the application id.
- **`extensions/jsfs`** (`js`): the page's `localStorage`, under the key
  `cog.storage.<AppId>`.

## No Platform Code

storage carries no platform code: its root, `storageimpl` and `internal` have
no build tags and do not import `os`. What persists is the Adapter's business,
and read mounts are plain `fs.FS` values the composition root chooses.

The reason is what a disk read mount does in a browser. This was established by
a probe built for `GOOS=js GOARCH=wasm` and run under the browser
`wasm_exec.js`, whose `fs` is the stub a browser tab gets:

- `os.DirFS(path).Open(name)` fails with `not implemented on js`. The error is
  `errors.ErrUnsupported`, **not** `fs.ErrNotExist`.
- The overlay falls through to the next mount only on `fs.ErrNotExist`
  (`FileSystem.Open`). So in a browser a disk read mount aborts every lookup
  that reaches it, instead of being skipped.
- `os.Executable` also fails on js ("Executable not implemented for js"), so an
  application id cannot be derived from the executable in a browser.

storage used to carry both: a `WithReadDiskFS` helper, an implicit read mount
of the executable's directory, and an application id defaulted from
`os.Executable`. They are gone. A desktop root that wants a directory mounts
`os.DirFS` itself, a browser root mounts what it preloaded, and the application
id belongs to the Adapter that uses it.

## Plugin

- Name: `storage.Name` (`"storage"`)
- Constructor: `storageimpl.New() kernel.Plugin`
- Plugin dependencies: none
- Requires: exactly one `storage.PermanentFS` Adapter
- Go package dependencies: `kernel` and the standard library
- Events published or subscribed: none

The plugin calls `registrar.RequireAdapter[storage.PermanentFS]()`. A
composition with no provider fails with `kernel.ErrMissingAdapter`, and one with
two fails with `kernel.ErrDuplicateAdapter`. No command installs a permanent
filesystem. The Adapter is bound after every `Register`, so the `FileSystem`
resource resolves it when it is read or written, which is from `Start` onwards.

Configuration is supplied under `storage.Name`:

```go
config := map[kernel.PluginName]any{
    storage.Name: storageimpl.DefaultConfig().
        WithReadFS("res", storage.DefaultReadPriority, os.DirFS("res")).
        WithReadFS("embedded", 100, embeddedFS),
}

plugins := []kernel.Plugin{
    storageimpl.New(),
    diskfs.New(diskfs.Config{AppId: "my-app"}), // or jsfs.New in a browser
    …
}
```

`storageimpl.Config` exposes `ReadMounts` and `ValuesPath`. Its `With*` methods
return modified copies. `DefaultReadPriority` is zero.

`PermanentMount` is reserved: the plugin derives it from the permanent
filesystem, and a `Config` that mounts it is rejected with `ErrReservedMount`.

## Adapters

An Adapter is a plugin that provides `storage.PermanentFS` during its
`Register`:

```go
registrar.ProvideAdapter[storage.PermanentFS](permanent)
```

Each of the two cog ships exports only `New` and `Config` (plus its
`ErrInvalidAppId`), and is built only for its platform.

- **`diskfs.New(diskfs.Config{AppId})`** opens `<data dir>/<AppId>`, creating
  it if needed: `%LOCALAPPDATA%` on Windows, `$XDG_DATA_HOME` (or
  `~/.local/share`) on Linux, the user config directory on macOS. An empty
  `AppId` is the executable's name without its extension. An `AppId` that is not
  one directory name fails `Register` with `diskfs.ErrInvalidAppId`. Every
  operation is confined to the directory through `os.Root`.
- **`jsfs.New(jsfs.Config{AppId})`** keeps the whole filesystem as one JSON
  document under `localStorage` key `cog.storage.<AppId>`. A browser has no
  executable to name the app after, so an empty `AppId` is an error, as is one
  that is not a single name: both fail `Register` with `jsfs.ErrInvalidAppId`.

## Resources

### `FileSystem`

The one storage resource. It is an immutable `fs.FS` overlay covering every
readable filesystem, including the permanent one, and it exposes no mutators.
`ReadMount{Id, Priority, FS}` entries are searched by descending priority; equal
priorities keep registration order. `PermanentMount` is searched ahead of every
other mount whatever their priorities, so a file that was just written is the
one read back — other mounts use `math.MaxInt` too (canvas mounts its shaders
there), and a priority tie must not shadow saved data. Only `fs.ErrNotExist`
falls through to the next mount. `Open` requires `fs.ValidPath` names.

There is one way to read: reading is never split by which filesystem holds the
file. Saved data reads back through `FileSystem` like any other asset.

Reads and writes are one resource on purpose. Two resources over the same bytes
gave the scheduler two lock types to arbitrate, so a per-frame reader and a
writer of the same file did not conflict and ran concurrently. With one
resource, a writer excludes every reader for the duration of the write.

`NewFileSystem(id, fs)` builds a standalone `FileSystem` over one filesystem,
with no permanent filesystem behind it, for tests and embedders.

### `WriteFS` and `WriteAccess`

Mutating the permanent filesystem requires a write lock, and the type system
enforces it: `WriteFS` is only produced by `WriteAccess`, which takes a
`kernel.Write[storage.FileSystem]`. A read lock cannot make one.

```go
var filesystem kernel.Write[storage.FileSystem]
return func(access kernel.ResourceAccess) {
        filesystem = access.GetWrite[storage.FileSystem]()
    }, func(_ kernel.Kernel, request SaveRequest) (SaveResponse, error) {
        data, _ := fs.ReadFile(filesystem.Get(), "save.json") // read: the resource itself
        write := storage.WriteAccess(filesystem)              // write: the capability
        return SaveResponse{}, write.WriteFile("save.json", data, 0o600)
    }
```

One lock covers both halves, so a handler that reads a file and writes it back
no longer declares two. `WriteFS` is the write side only; reading goes through
the `FileSystem` value, which the same handle yields from `Get`.

### `PermanentFS`

The Adapter interface:

```go
interface {
    fs.FS
    WriteFile(string, []byte, fs.FileMode) error
    MkdirAll(string, fs.FileMode) error
    Remove(string) error
    Rename(string, string) error
}
```

All operation names use `fs.ValidPath` form. `Rename` onto an existing file
replaces it, which is how a save is made atomic. An Adapter is never handed back
to callers.

### `Values`

The key-value store backing the value commands. It caches the values file after
the first read, so a read populates the cache and handlers declare write access
to it either way.

Resource values and opened files must remain inside the current handler's lock
scope. Mount management goes through the commands below, since the mount list
can only be mutated that way; reading and writing files is ordinary direct
resource access.

## Commands Implemented

| Command | Request / response | Resource access | Behavior |
| --- | --- | --- | --- |
| `SetMountCmd` | `SetMountRequest{Mount}` / `SetMountResponse` | write `FileSystem` | Adds or replaces a mount by `MountId`. Rejects `PermanentMount`. |
| `RemoveMountCmd` | `RemoveMountRequest{Id}` / `RemoveMountResponse{Removed}` | write `FileSystem` | Removes a mount and reports whether it existed. Rejects `PermanentMount`. |
| `AccessValuesCmd` | `GetValue(key, default, out)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Reads one value, loading the values file on first use. |
| `AccessValuesCmd` | `SetValue(key, value)`, `SetValueNoFlush(key, value)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Stores one value, flushing unless batched; `Found` reports a replacement. |
| `AccessValuesCmd` | `DeleteValue(key)`, `DeleteValueNoFlush(key)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Removes one key, flushing unless batched; `Found` reports it existed. |
| `AccessValuesCmd` | `FlushValues()` / `AccessValuesResponse{}` | write `FileSystem`, write `Values` | Writes pending value changes; a no-op when nothing changed. |

`AccessValuesCmd` is one command over a closed union of store operations, built
only by the constructors above. Every operation may load the values file and
write it back, so they share one command and one pair of locks rather than
splitting a single store into several entry points.

## Errors

- `storageimpl.ErrInvalidConfig{Got}`: plugin configuration is not a
  `storageimpl.Config`.
- `ErrInvalidMount{Id}`: a mount has an empty ID or nil filesystem.
- `ErrReservedMount{Id}`: `PermanentMount` was mounted or unmounted by hand.
- `ErrNoWriteAccess{Op, Path}`: `WriteAccess` received a handle whose write lock
  was never declared.
- `ErrInvalidValuesPath{Path}`: the values file path is not an `fs.ValidPath`.
- `ErrInvalidValuesFile{Path, Err}`: the values file is not a JSON object.
- `ErrInvalidKey`, `ErrInvalidValueRequest`, `ErrInvalidOutValue{Key}`:
  malformed value requests.
- `diskfs.ErrInvalidAppId{AppId}`, `jsfs.ErrInvalidAppId{AppId}`: the Adapter's
  application id is invalid.

Each exported error type implements `Error() string`.
