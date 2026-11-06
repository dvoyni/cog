package headless

import "github.com/dvoyni/cog/platform"

// Windows is a platform.WindowsSocket whose windows exist only as platform's
// window state: a window opens at its requested size, at scale 1, with the
// zero native handle, and is gone once closed. platform calls it on the main
// thread alone.
type Windows struct {
	// changes are what the next Pump reports: a window opened for each Open
	// since the last.
	changes []platform.InputChange
}

// NewWindows answers headless windows, none open, for a test's plugin to
// bind to platform.WindowsSocket.
func NewWindows() *Windows {
	return &Windows{}
}

// Open reports window open from the next Pump, at entry's size, its
// framebuffer as large at scale 1, with the zero native handle.
func (w *Windows) Open(window platform.Window, entry platform.WindowEntry) error {
	w.changes = append(w.changes, platform.InputChange{
		Kind:            platform.InputChangeWindowOpened,
		Window:          window,
		Size:            entry.Size,
		FramebufferSize: entry.Size,
		Scale:           1,
	})
	return nil
}

// Change applies nothing: a headless window has nothing to show its entry
// on.
func (w *Windows) Change(window platform.Window, previous, next platform.WindowEntry) error {
	return nil
}

// SetCursor applies nothing: a headless window has no cursor.
func (w *Windows) SetCursor(window platform.Window, cursor platform.CursorRequest) error {
	return nil
}

// EffectiveCursorMode answers platform.CursorModeNormal: a headless window
// never has the focus.
func (w *Windows) EffectiveCursorMode(window platform.Window) platform.CursorMode {
	return platform.CursorModeNormal
}

// SetCursorPosition moves nothing: a headless window has no cursor.
func (w *Windows) SetCursorPosition(window platform.Window, position platform.Point) error {
	return nil
}

// Close forgets nothing, for the window is only platform's state, and
// reports nothing, as platform.WindowsSocket says.
func (w *Windows) Close(window platform.Window) error {
	return nil
}

// Pump appends the windows opened since the last Pump to changes, and answers
// it.
func (w *Windows) Pump(changes []platform.InputChange) []platform.InputChange {
	changes = append(changes, w.changes...)
	w.changes = w.changes[:0]
	return changes
}
