// Package platform is what the operating system provides to a kernel,
// declared as sockets implementing plugins bind: the window implementation,
// the input source, the writable filesystem and the read mounts.
// platform holds no platform code; it owns what is the same for every
// implementation.
//
// The implementing plugins bind to platform's sockets in Init, wherever they
// are listed in Spawn: the window implementation with
// configurator.BindSocket[platform.WindowsSocket], the input source with
// configurator.BindSocket[platform.InputSourceSocket], the writable
// filesystem with configurator.BindSocket[platform.WriteFSSocket] and read
// mounts with configurator.BindSocket(platform.ReadMountSocket{...}).
// Windows and input are optional, so a kernel of the files alone can read
// the values; platform puts the files behind the FS resource.
//
// A job opens a window with Open on the *WindowManager under its write lock,
// and gets the window's Window at once. It changes the window with Change or
// the setters of one field, and closes it with Close. Nothing reaches the
// operating system until ProcessEvents runs: it applies what changed in the
// request through the WindowsSocket, lets it pump, collects the input
// changes, folds them into the manager's window state under the write lock,
// then publishes WindowOpenedEvent, WindowClosedEvent and CloseRequestedEvent
// in the order the changes arrived. A close request closes nothing: the
// application calls Close.
//
// ProcessEvents is pinned to the main thread and scheduled nowhere by
// platform. The application schedules it, from the Tasks resource, in the
// track handlers it chooses, and returns it in the out dependency:
//
//	return event, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
//
// A job reads files under kernel.Read[platform.FS], with fs.ReadFile,
// fs.ReadDir and the other fs helpers. Under kernel.Write[platform.FS],
// ElevateFileAccess gives the WriteFS that writes, into the writable
// filesystem; nothing else does. The *Values resource keeps a JSON file in
// FS, named by Config.ValuesFile: Get takes the FS, Set, Delete and Flush
// the WriteFS. The file is read once and kept; changes are written by Flush,
// and by platform itself when a window closes.
//
// See docs/design.md.
package platform
