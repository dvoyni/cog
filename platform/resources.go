package platform

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// Tasks is the resource holding every task platform offers others to
// schedule.
type Tasks struct {
	// ProcessEvents is the pump: a task pinned to the main thread that
	// platform schedules nowhere. A handler that schedules it returns it in
	// its out dependency, so the track's in-flight limit sees it:
	//
	//	return event, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
	ProcessEvents *kernel.Task[kernel.NoArg, kernel.NoResult]
}

// Windows is the window seam: it opens, changes and closes operating-system
// windows and pumps their events. Exactly one plugin, listed before platform
// in Spawn, installs it in its Init with
// configurator.InitResource[platform.Windows]. Only ProcessEvents calls it,
// on the main thread.
type Windows interface {
	// Open creates window's operating-system window as entry asks. It reports
	// the window open with an InputChangeWindowOpened from a later Pump, at the
	// latest the one of this run.
	Open(window Window, entry WindowEntry) error
	// Change applies to window what differs between previous and next, its
	// entry as last applied and as requested now.
	Change(window Window, previous, next WindowEntry) error
	// Close destroys window's operating-system window. platform publishes its
	// WindowClosedEvent itself; Close reports nothing.
	Close(window Window) error
	// Pump handles the operating system's pending events, appends the window
	// changes they make to changes, and answers it.
	Pump(changes []InputChange) []InputChange
}

// InputSource is the input seam: it reports the input changes since the last
// ProcessEvents run. Exactly one plugin, listed before platform in Spawn,
// installs it in its Init with configurator.InitResource[platform.InputSource].
// Only ProcessEvents calls it, on the main thread, after Windows.Pump.
type InputSource interface {
	// AppendChanges appends the input changes since its last call to changes,
	// in the order they happened, and answers it.
	AppendChanges(changes []InputChange) []InputChange
}

// FS is the resource of every file: the writable filesystem and the read
// mounts stacked into one overlay. The writable filesystem is searched first,
// then the read mounts by priority; a listing merges every layer, a name in
// several showing the highest layer's entry. It is fs.FS itself, so a job
// reads it under kernel.Read[platform.FS] with fs.ReadFile, fs.ReadDir,
// fs.Stat, fs.Glob and fs.WalkDir; files opened from it are valid only while
// the job runs.
type FS = fs.FS

// ElevateFileAccess answers the FS a job holds the write lock on as a
// WriteFS, whose writes land in the writable filesystem. Only the write
// handle gives it, so a job holding the read lock cannot write.
func ElevateFileAccess(handle kernel.Write[FS]) WriteFS {
	return writableFileSystem{fileSystem: handle.Get().(*fileSystem)}
}

// WriteFS is an FS that writes. ElevateFileAccess gives the FS resource as
// one, its writes landing in the writable filesystem; it is no resource.
type WriteFS interface {
	FS
	// WriteFile writes data to name, creating it with perm or truncating it.
	WriteFile(name string, data []byte, perm fs.FileMode) error
	// MkdirAll creates the directory path and any parents it lacks with perm.
	MkdirAll(path string, perm fs.FileMode) error
	// Remove removes the file or empty directory name.
	Remove(name string) error
	// Rename moves oldName to newName, replacing a file there.
	Rename(oldName, newName string) error
	// AppendFile appends data to name, creating it with perm.
	AppendFile(name string, data []byte, perm fs.FileMode) error
}
