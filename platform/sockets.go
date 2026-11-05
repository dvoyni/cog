package platform

// WindowsSocket is the window seam: it opens, changes and closes
// operating-system windows and pumps their events. At most one plugin binds
// it, in its Init, with configurator.BindSocket[platform.WindowsSocket]; a
// kernel with none, such as one spawned only to read the values, applies no
// window request. Only ProcessEvents calls it, on the main thread.
type WindowsSocket interface {
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

// InputSourceSocket is the input seam: it reports the input changes since
// the last ProcessEvents run. At most one plugin binds it, in its Init, with
// configurator.BindSocket[platform.InputSourceSocket]; a kernel with none
// reports no input. Only ProcessEvents calls it, on the main thread, after
// WindowsSocket.Pump.
type InputSourceSocket interface {
	// AppendChanges appends the input changes since its last call to changes,
	// in the order they happened, and answers it.
	AppendChanges(changes []InputChange) []InputChange
}

// WriteFSSocket is the writable filesystem, which exactly one plugin binds in
// its Init with configurator.BindSocket[platform.WriteFSSocket]. It is
// searched before every read mount, and writes land in it. It is no
// resource: jobs write through ElevateFileAccess, so the lock on the FS
// resource keeps readers from seeing a file mid-write.
type WriteFSSocket interface {
	WriteFS
}

// ReadMountSocket is one read-only layer of the FS resource, which any
// plugin binds in its Init with
// configurator.BindSocket(platform.ReadMountSocket{...}): an embed.FS, an
// os.DirFS, any fs.FS. Higher priorities are searched first, equal ones in
// plugin order, all after the writable filesystem.
type ReadMountSocket struct {
	FS       FS
	Priority int
}
