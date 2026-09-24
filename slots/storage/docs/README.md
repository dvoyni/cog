# storage

`github.com/dvoyni/cog/slots/storage` provides one kernel resource,
`FileSystem`: a prioritized read-only overlay over every mounted filesystem,
including the single permanent one that writes land in.

storage is a **Slot**: it ships its own declarations and implementation, and
works only once a `PermanentFS` **Adapter** fills its required
`PermanentFSPort`. The vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the
decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

storage has the alias-index root of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md)
and [ADR 0003](../../../docs/adr/0003-roots-are-alias-indexes.md).

- **`slots/storage`** is the root, and declares nothing: it aliases what
  `internal/` declares — the commands, `Config`, `FileSystem`, `Values`,
  `WriteFS`, `ReadMount`, `PermanentFS` and `PermanentFSPort`, `ReadMountPort`,
  the errors and `Name`. Its functions, the value-request builders,
  `WriteAccess` and `NewFileSystem`, are forwarders in `utils.go`. It is what
  every other package imports.
- **`slots/storage/internal`** is the plugin: everything the root aliases,
  among them `FileSystem`, `WriteFS`, `Values`, the value requests and what
  they refer to, whose unexported state the handlers read, beside its `New`,
  configuration resolution and the command handlers. It never imports the
  root.
- **`slots/storage/storageplugin`** exports only `New() kernel.Plugin`. Only
  composition roots and tests import it.

The Adapters are Extensions in `extensions/`:

- **`extensions/diskstorage`** (`!js`): a directory under the user's data
  directory, named by the application id.
- **`extensions/jsstorage`** (`js`): the page's `localStorage`, under the key
  `cog.storage.<AppId>`.

## No Platform Code

storage carries no platform code: none of its packages has build tags or
imports `os`. What persists is the Adapter's business, and read mounts are
plain `fs.FS` values that plugins contribute.

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
`os.Executable`. They are gone. A desktop game that wants a directory mounts
`os.DirFS` itself, a browser game mounts what it preloaded, and the application
id belongs to the Adapter that uses it.

## Plugin

- Name: `storage.Name` (`"storage"`)
- Constructor: `storageplugin.New() kernel.Plugin`
- Plugin dependencies: none
- Requires: exactly one Adapter for `storage.PermanentFSPort`
- Collects: any number of Adapters for `storage.ReadMountPort`, zero included
- Go package dependencies: `kernel` and the standard library
- Events published or subscribed: none

The plugin calls `registrar.RequireAdapter[storage.PermanentFSPort]()`. A
composition with no provider fails with `kernel.ErrMissingAdapter`, and one with
two fails with `kernel.ErrDuplicateAdapter`. No command installs a permanent
filesystem. The Adapter is bound after every `Register`, so the `FileSystem`
resource resolves it when it is read or written, which is from `Start` onwards.

Configuration is a `storage.Config` supplied under `storage.Name`. Its zero
value is the default: `DefaultValuesPath`. `storage.Config` exposes only
`ValuesPath`, and `WithValuesPath` returns a modified copy.

```go
config := map[kernel.PluginName]any{
    diskstorage.Name: diskstorage.Config{AppId: "my-app"},
}

plugins := []kernel.Plugin{
    storageplugin.New(),
    diskstorageplugin.New(), // or jsstorageplugin.New() in a browser, with jsstorage.Config under jsstorage.Name
    mygame.New(os.DirFS("res")), // contributes its read mount
    …
}
```

### Read mounts

Read mounts are not configured: plugins contribute them through
`storage.ReadMountPort`, a collected Port built on `ReadMount` itself. A
composition root has no `Register` to provide from, so the mounts a game needs
come from the game's own plugin, which is handed the filesystem by `main`, the
one place that knows the platform. A plugin offers an Adapter for the Port in
its root's `adapters.go` and provides one `ReadMount` per mount during its
`Register`:

```go
type StorageReadMount kernel.Adapter[storage.ReadMountPort]

registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
    Id: "res", Priority: storage.DefaultReadPriority, FS: res,
})
registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
    Id: "embedded", Priority: 100, FS: embeddedFS,
})
```

One plugin may contribute any number, and so may any number of plugins; canvas
and scene contribute their built-in shaders and font the same way.
`DefaultReadPriority` is zero.

Adapters bind after every `Register`, so storage installs the mounts at its
`Start`. storage has no dependencies, so it starts ahead of every plugin that
depends on it, and those find the mounts in place. The `Start` fails, and with
it the run, when:

- one id is contributed more than once, by one plugin or several:
  `ErrDuplicateMount{Id, Plugins}`. Nothing is mounted, since plugin order is
  not a choice anyone makes and must not pick a winner;
- a mount has an empty id or a nil `FS`: `ErrInvalidMount`;
- a mount claims `PermanentMount`, which the plugin derives from the permanent
  filesystem: `ErrReservedMount`.

`SetMountCmd` and `RemoveMountCmd` still add, replace and remove mounts by id
once the engine runs.

## Adapters

An Extension declares an Adapter type for `storage.PermanentFSPort` in its
`adapters.go` and provides a `storage.PermanentFS` under it during its
`Register`:

```go
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort]

registrar.ProvideAdapter[StoragePermanentFS](permanent)
```

The two cog ships are each built only for their platform.

- **diskstorage** is an Extension with an alias-index root. Its root,
  `extensions/diskstorage`, offers only `Name`, `Config`, the
  `StoragePermanentFS` Adapter and its errors, aliased from its `internal/`,
  where the plugin is too; `diskstorageplugin.New()` constructs it. Its `diskstorage.Config` is
  supplied under `diskstorage.Name`, and its zero value is the default. The
  plugin opens `<data dir>/<AppId>`, creating it if needed: `%LOCALAPPDATA%` on
  Windows, `$XDG_DATA_HOME` (or `~/.local/share`) on Linux, the user config
  directory on macOS. An empty `AppId` is the executable's name without its
  extension. An `AppId` that is not one directory name fails `Register` with
  `diskstorage.ErrInvalidAppId`, and a config value that is not a
  `diskstorage.Config` with `diskstorage.ErrInvalidConfig`. Every operation is
  confined to the directory through `os.Root`.
- **jsstorage** is an Extension in the same shape. Its root,
  `extensions/jsstorage`, offers only `Name`, `Config`, the
  `StoragePermanentFS` Adapter and its errors, aliased from its `internal/`,
  where the plugin is too; `jsstorageplugin.New()` constructs it. Its
  `jsstorage.Config` is supplied under `jsstorage.Name`. The plugin keeps the
  whole filesystem as one JSON document under `localStorage` key
  `cog.storage.<AppId>`. A browser has no executable to name the app after, so
  an empty `AppId`, the zero value included, is an error, as is one that is not
  a single name: both fail `Register` with `jsstorage.ErrInvalidAppId`. A config
  value that is not a `jsstorage.Config` fails it with
  `jsstorage.ErrInvalidConfig`.

## Resources

### `FileSystem`

The one storage resource. It is an immutable `fs.FS` overlay covering every
readable filesystem, including the permanent one, and it exposes no mutators.
`ReadMount{Id, Priority, FS}` entries are searched by descending priority; equal
priorities keep registration order. `PermanentMount` is searched ahead of every
other mount whatever their priorities, so a file that was just written is the
one read back — other mounts use `math.MaxInt` too (canvas contributes its
shaders there), and a priority tie must not shadow saved data. Only `fs.ErrNotExist`
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

- `ErrInvalidConfig{Got}`: plugin configuration is not a `storage.Config`.
- `ErrInvalidMount{Id}`: a mount has an empty ID or nil filesystem.
- `ErrReservedMount{Id}`: `PermanentMount` was mounted or unmounted by hand.
- `ErrDuplicateMount{Id, Plugins}`: a mount id was contributed through
  `ReadMountPort` more than once; `Plugins` lists every contributor in plugin
  order.
- `ErrNoWriteAccess{Op, Path}`: `WriteAccess` received a handle whose write lock
  was never declared.
- `ErrInvalidValuesPath{Path}`: the values file path is not an `fs.ValidPath`.
- `ErrInvalidValuesFile{Path, Err}`: the values file is not a JSON object.
- `ErrInvalidKey`, `ErrInvalidValueRequest`, `ErrInvalidOutValue{Key}`:
  malformed value requests.
- `diskstorage.ErrInvalidAppId{AppId}`, `jsstorage.ErrInvalidAppId{AppId}`: the Adapter's
  application id is invalid.
- `diskstorage.ErrInvalidConfig{Got}`, `jsstorage.ErrInvalidConfig{Got}`: the value
  under the Adapter's `Name` is not its `Config`.

Each exported error type implements `Error() string`.
