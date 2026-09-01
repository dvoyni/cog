# storage

`github.com/cog-engine/storage` provides a prioritized read-only filesystem
overlay and one permanent writable filesystem as kernel resources.

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
    WithWriteFS(customWriteFS)
```

`Config` exposes `AppId`, `ReadMounts`, and `WriteFS`. Its `With*` methods return
modified copies. If no `ExecutableMount` is configured, the plugin adds the
platform default at lowest priority. If `WriteFS` is nil, it opens the
platform-specific application data directory. The resolved `WriteFS` is also
mounted into `ReadFS` as `PermanentMount` at highest priority, unless the
configuration already supplies that mount, so a written file is the one read
back. `DefaultReadPriority` is zero.

## Resources

### `ReadFS`

An immutable `fs.FS` overlay covering every readable filesystem, including the
permanent one. `ReadMount{Id, Priority, FS}` entries are searched by descending
priority; equal priorities keep registration order. Only `fs.ErrNotExist` falls
through to the next mount. `ReadFS.Open` requires `fs.ValidPath` names.

There is one way to read: reading is never split by which filesystem holds the
file. Saved data reads back through `ReadFileCmd` like any other asset.

Because the permanent filesystem is reachable under the `ReadFS` lock while a
writer holds the `WriteFS` lock, a `WriteFS` implementation must keep no mutable
in-process state of its own; the bundled disk and localStorage backends resolve
their contents on each call.

### `WriteFS`

A public alias for the permanent filesystem interface:

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
confined `WriteFS`. All operation names use `fs.ValidPath` form. `WriteFS` is
the write side only; it is reached for reading through its `ReadFS` mount.

Resource values and opened files must remain inside the current handler's lock
scope. Prefer the commands below over direct resource access: a handler declares
`access.Uses[storage.ReadFileCmd]()` and never names `ReadFS` or `WriteFS`
itself. The resource-access column below is what each command locks for you.

## Commands Implemented

| Command | Request / response | Resource access | Behavior |
| --- | --- | --- | --- |
| `SetReadFSCmd` | `SetReadFSRequest{Mount}` / `SetReadFSResponse` | write `ReadFS` | Adds or replaces a mount by `MountId`. |
| `RemoveReadFSCmd` | `RemoveReadFSRequest{Id}` / `RemoveReadFSResponse{Removed}` | write `ReadFS` | Removes a mount and reports whether it existed. |
| `SetWriteFSCmd` | `SetWriteFSRequest{FS}` / `SetWriteFSResponse` | write `WriteFS` | Replaces the permanent filesystem. |
| `GetReadFSCmd` | `GetReadFSRequest{Use func(fs.FS) error}` / `GetReadFSResponse` | read `ReadFS` | Invokes `Use` synchronously under the read lock. |
| `ReadFileCmd` | `ReadFileRequest{Name}` / `ReadFileResponse{Data}` | read `ReadFS` | Reads and closes the first matching file. |
| `WriteFileCmd` | `WriteFileRequest{Name, Data, Perm}` / `WriteFileResponse` | write `WriteFS` | Writes a complete file. |
| `MkdirAllCmd` | `MkdirAllRequest{Path, Perm}` / `MkdirAllResponse` | write `WriteFS` | Creates a directory and parents. |
| `RemoveCmd` | `RemoveRequest{Name}` / `RemoveResponse` | write `WriteFS` | Removes a file or empty directory. |
| `RenameCmd` | `RenameRequest{OldName, NewName}` / `RenameResponse` | write `WriteFS` | Renames within the permanent filesystem. |

The callback passed to `GetReadFSCmd` must not retain the filesystem or an open
file after returning.

## Errors

- `ErrInvalidConfig{Got}`: plugin configuration is not a `storage.Config`.
- `ErrInvalidMount{Id}`: a mount has an empty ID or nil filesystem.
- `ErrInvalidAppId{AppId}`: the application ID is not one directory name.
- `ErrInvalidWriteFS`: a nil writable filesystem was supplied.
- `ErrInvalidReadFSCallback`: `GetReadFSCmd` received a nil callback.

Each exported error type implements `Error() string`.
