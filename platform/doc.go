// Package platform is what the operating system provides to a kernel,
// declared as interfaces implementing plugins fill: Windows and InputSource.
// platform holds no platform code; it owns what is the same for every
// implementation.
//
// The implementing plugins are listed before platform in Spawn and install
// their implementations in Init, with configurator.InitResource[platform.Windows]
// and configurator.InitResource[platform.InputSource]; platform's Init finds
// them, and panics if either is missing.
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
// platform. The application schedules it, from the Pump resource, in the
// track handlers it chooses, and returns it in the out dependency:
//
//	return event, ex.Join(in, ex.Schedule(pump.ProcessEvents, kernel.NoArg{}).Handle())
//
// See docs/design.md.
package platform
