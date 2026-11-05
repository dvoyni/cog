// Package platform is what the operating system provides to a kernel,
// declared as interfaces implementing plugins fill: Windows and InputSource,
// the writable filesystem and the read mounts.
// platform holds no platform code; it owns what is the same for every
// implementation.
//
// The window implementations are listed before platform in Spawn and install
// platform.Windows and platform.InputSource in Init with
// configurator.InitResource; platform's Init finds them, and panics if either
// is missing. Any plugin, wherever it is listed, binds the writable
// filesystem with configurator.BindSocket[platform.WriteFSSocket] and read
// mounts with configurator.BindSocket(platform.ReadMountSocket{...});
// platform puts them behind the FS resource.
//
// A job opens a window with Open on the *WindowManager under its write lock,
// and gets the window's Window at once. It changes the window with Change or
// the setters of one field, and closes it with Close. Nothing reaches the
// operating system until ProcessEvents runs: it applies what changed in the
// request through Windows, lets Windows pump, collects the input changes,
// folds them into the manager's window state under the write lock, then
// publishes WindowOpenedEvent, WindowClosedEvent and CloseRequestedEvent in
// the order the changes arrived. A close request closes nothing: the
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
