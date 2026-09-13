# storage

`github.com/cog-engine/storage` provides one kernel resource, `FileSystem`: a
prioritized read-only overlay over every mounted filesystem, including the
single permanent one that writes land in.

## Plugin

- Name: `storage.Name` (`"storage"`)
- Constructor: `storage.New() *storage.Plugin`
- Plugin dependencies: none
- Go package dependencies: `kernel` and the standard library
- Events published or subscribed: none

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`, and
`Init`.

Configuration is supplied under `storage.Name`:

```go
cfg := storage.DefaultConfig("my-app").
    WithReadDiskFS("res").
    WithReadFS("embedded", 100, embeddedFS).
    WithPermanentFS(customPermanentFS)
```

`Config` exposes `AppId`, `ReadMounts`, and `PermanentFS`. Its `With*` methods
return modified copies. If no `ExecutableMount` is configured, the plugin adds
the platform default at lowest priority. If `PermanentFS` is nil, it opens the
platform-specific application data directory. `DefaultReadPriority` is zero.

`PermanentMount` is reserved: the plugin derives it from the permanent
filesystem, and a `Config` that mounts it is rejected with `ErrReservedMount`.

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

What a permanent filesystem backend implements:

```go
interface {
    fs.FS
    WriteFile(string, []byte, fs.FileMode) error
    MkdirAll(string, fs.FileMode) error
    Remove(string) error
    Rename(string, string) error
}
```

`OpenDiskFS(path)` creates the root directory if necessary and returns a
confined `PermanentFS`. All operation names use `fs.ValidPath` form. Backends
are supplied to storage through `Config` or `SetPermanentFSCmd` and are never
handed back to callers.

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
| `SetPermanentFSCmd` | `SetPermanentFSRequest{FS}` / `SetPermanentFSResponse` | write `FileSystem` | Replaces the permanent filesystem and its overlay mount in one step. |
| `AccessValuesCmd` | `GetValue(key, default, out)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Reads one value, loading the values file on first use. |
| `AccessValuesCmd` | `SetValue(key, value, skipFlush)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Stores one value, flushing unless batched; `Found` reports a replacement. |
| `AccessValuesCmd` | `DeleteValue(key, skipFlush)` / `AccessValuesResponse{Found}` | write `FileSystem`, write `Values` | Removes one key, flushing unless batched; `Found` reports it existed. |
| `AccessValuesCmd` | `FlushValues()` / `AccessValuesResponse{}` | write `FileSystem`, write `Values` | Writes pending value changes; a no-op when nothing changed. |

`AccessValuesCmd` is one command over a closed union of store operations, built
only by the four constructors above. Every operation may load the values file
and write it back, so they share one command and one pair of locks rather than
splitting a single store into four entry points.

## Errors

- `ErrInvalidConfig{Got}`: plugin configuration is not a `storage.Config`.
- `ErrInvalidMount{Id}`: a mount has an empty ID or nil filesystem.
- `ErrReservedMount{Id}`: `PermanentMount` was mounted or unmounted by hand.
- `ErrInvalidAppId{AppId}`: the application ID is not one directory name.
- `ErrInvalidPermanentFS`: a nil permanent filesystem was supplied.
- `ErrNoWriteAccess{Op, Path}`: `WriteAccess` received a handle whose write lock
  was never declared.
- `ErrInvalidValuesPath{Path}`: the values file path is not an `fs.ValidPath`.
- `ErrInvalidValuesFile{Path, Err}`: the values file is not a JSON object.
- `ErrInvalidKey`, `ErrInvalidValueRequest`, `ErrInvalidOutValue{Key}`:
  malformed value requests.

Each exported error type implements `Error() string`.
