package headless

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/platform"
)

// Windows is a platform.WindowsSocket whose windows exist only as platform's
// window state: a window opens at its requested size, at scale 1, with the
// zero native handle, takes the size a test gives it with Resize, keeps the
// rest of its entry as it was last applied, and is gone once closed. platform
// calls it on the main thread; a test may call Resize and IsHidden from any
// goroutine.
type Windows struct {
	mutex sync.Mutex
	// entries holds each open window's entry as last applied.
	entries map[platform.Window]platform.WindowEntry
	// changes are what the next Pump reports: a window opened for each Open,
	// and a resize for each Resize, since the last.
	changes []platform.InputChange
}

// NewWindows answers headless windows, none open, for a test's plugin to
// bind to platform.WindowsSocket.
func NewWindows() *Windows {
	return &Windows{entries: map[platform.Window]platform.WindowEntry{}}
}

// Open reports window open from the next Pump, at entry's size, its
// framebuffer as large at scale 1, with the zero native handle.
func (w *Windows) Open(window platform.Window, entry platform.WindowEntry) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.entries[window] = entry
	w.changes = append(w.changes, platform.InputChange{
		Kind:            platform.InputChangeWindowOpened,
		Window:          window,
		Size:            entry.Size,
		FramebufferSize: entry.Size,
		Scale:           1,
	})
	return nil
}

// Change keeps next as window's entry, which IsHidden reads; it resizes
// nothing, for only a test's Resize does.
func (w *Windows) Change(window platform.Window, previous, next platform.WindowEntry) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.entries[window] = next
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

// Close forgets window and drops what the next Pump would have reported of
// it, and reports nothing, as platform.WindowsSocket says.
func (w *Windows) Close(window platform.Window) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	delete(w.entries, window)
	w.changes = slices.DeleteFunc(w.changes, func(change platform.InputChange) bool { return change.Window == window })
	return nil
}

// Pump appends the windows opened and resized since the last Pump to changes,
// and answers it.
func (w *Windows) Pump(changes []platform.InputChange) []platform.InputChange {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	changes = append(changes, w.changes...)
	w.changes = w.changes[:0]
	return changes
}

// Resize reports window resized from the next Pump, to size in window space,
// framebufferSize in pixels, at scale. A window that is not open, or closed
// before the Pump, takes no resize.
func (w *Windows) Resize(window platform.Window, size, framebufferSize platform.Size, scale float32) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if _, open := w.entries[window]; !open {
		return
	}
	w.changes = append(w.changes, platform.InputChange{
		Kind:            platform.InputChangeResize,
		Window:          window,
		Size:            size,
		FramebufferSize: framebufferSize,
		Scale:           scale,
	})
}

// IsHidden answers whether window's entry, as last applied, keeps it hidden.
// A window that is not open is not hidden.
func (w *Windows) IsHidden(window platform.Window) bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.entries[window].Hidden
}
