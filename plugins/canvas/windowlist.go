package canvas

import "github.com/dvoyni/cog/plugins/platform"

// windowList is canvas's private account of the windows platform has
// opened and not yet closed, in the order they opened, kept by the tasks
// following platform.WindowOpenedEvent and platform.WindowClosedEvent: the
// first is the main window, the default camera's target, which is gogpu's
// window opened at start when it has one.
type windowList struct {
	windows []platform.Window
}

// mainWindow answers the window opened first of those still open, and the
// zero Window, which names none, when none is.
func (l *windowList) mainWindow() platform.Window {
	if len(l.windows) == 0 {
		return platform.Window{}
	}
	return l.windows[0]
}
