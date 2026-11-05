package platform

import "github.com/dvoyni/cog/kernel"

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

// Pump is the resource holding ProcessEvents, the pump: a task pinned to the
// main thread that platform schedules nowhere. A handler that schedules it
// returns it in its out dependency, so the track's in-flight limit sees it:
//
//	return event, ex.Join(in, ex.Schedule(pump.ProcessEvents, kernel.NoArg{}).Handle())
type Pump struct {
	ProcessEvents *kernel.Task[kernel.NoArg, kernel.NoResult]
}
