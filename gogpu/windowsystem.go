package gogpu

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
)

// windowSystem is the plugin's platform.WindowsSocket: it opens, changes and
// closes the HostLoop's windows, pumps their events, reports their sizes and
// focus, and their pointers through the input source. platform calls it on
// the main thread alone.
type windowSystem struct {
	// plugin holds the HostLoop, the windows open and what the next Pump
	// reports.
	plugin *Plugin
}

// Open opens window with its surface through the HostLoop, at entry's title,
// size and fullscreen, gogpu's default size for a zero width or height, and
// shows it unless entry hides it. It reports the window open from the next
// Pump, at its sizes, and from later Pumps each resize, a scale change
// included, and each gain and loss of the focus. The user's close of the
// window is reported, and keeps it open, and its keys and pointer from the
// input source's next AppendChanges. It answers an error before gfx's adapter
// request, which starts the HostLoop.
func (w windowSystem) Open(window platform.Window, entry platform.WindowEntry) error {
	if w.plugin.loop == nil {
		return errors.New("gogpu: a window opens only once gfx has requested the adapter")
	}
	config := host.DefaultConfig()
	config.Title = entry.Title
	if entry.Size.Width > 0 && entry.Size.Height > 0 {
		config.Width = entry.Size.Width
		config.Height = entry.Size.Height
	}
	config.Fullscreen = entry.Fullscreen
	opened, err := w.plugin.loop.NewWindow(config)
	if err != nil {
		return err
	}
	opened.SetOnClose(func() bool { return w.requestClose(window) })
	opened.SetOnResize(func(int, int) { w.reportSizes(platform.InputChangeResize, window, opened) })
	opened.SetOnFocus(func(focused bool) { w.reportFocus(window, focused) })
	opened.SetOnKeyPress(func(key gpucontext.Key, modifiers gpucontext.Modifiers) {
		inputSource(w).reportKey(window, key, modifiers, true)
	})
	opened.SetOnKeyRelease(func(key gpucontext.Key, modifiers gpucontext.Modifiers) {
		inputSource(w).reportKey(window, key, modifiers, false)
	})
	opened.SetOnPointer(func(event gpucontext.PointerEvent) { inputSource(w).reportPointer(window, event) })
	if !entry.Hidden {
		opened.Show()
	}
	w.plugin.windows[window] = opened
	w.reportSizes(platform.InputChangeWindowOpened, window, opened)
	return nil
}

// Change applies to window's gogpu window what differs between previous and
// next: it shows the window first, sets its title, leaves fullscreen before
// resizing and enters it after, and hides the window last. gogpu ignores a
// size while the window is fullscreen, so the size is applied only to a
// window not fullscreen, and again each time it leaves fullscreen; a zero
// width or height leaves it as it is. It answers an error for a window not
// open.
func (w windowSystem) Change(window platform.Window, previous, next platform.WindowEntry) error {
	changed, ok := w.plugin.windows[window]
	if !ok {
		return fmt.Errorf("gogpu: changing %v, which is not open", window)
	}
	if previous.Hidden && !next.Hidden {
		changed.Show()
	}
	if previous.Title != next.Title {
		changed.SetTitle(next.Title)
	}
	if previous.Fullscreen && !next.Fullscreen {
		changed.SetFullscreen(false)
	}
	if !next.Fullscreen && (previous.Fullscreen || previous.Size != next.Size) && next.Size.Width > 0 && next.Size.Height > 0 {
		changed.SetSize(next.Size.Width, next.Size.Height)
	}
	if !previous.Fullscreen && next.Fullscreen {
		changed.SetFullscreen(true)
	}
	if !previous.Hidden && next.Hidden {
		changed.Hide()
	}
	return nil
}

// SetCursor answers an error naming it: the cursor is not built yet.
func (windowSystem) SetCursor(platform.Window, platform.CursorRequest) error {
	return unbuilt("SetCursor")
}

// EffectiveCursorMode answers platform.CursorModeNormal: the cursor is not
// built yet.
func (windowSystem) EffectiveCursorMode(platform.Window) platform.CursorMode {
	return platform.CursorModeNormal
}

// SetCursorPosition answers an error naming it: the cursor is not built yet.
func (windowSystem) SetCursorPosition(platform.Window, platform.Point) error {
	return unbuilt("SetCursorPosition")
}

// Close destroys window's gogpu window at once, with its surface, and drops
// what the next Pump would have reported of it. It reports nothing, as
// platform.WindowsSocket says; a window not open is left alone.
func (w windowSystem) Close(window platform.Window) error {
	closed, ok := w.plugin.windows[window]
	if !ok {
		return nil
	}
	delete(w.plugin.windows, window)
	w.plugin.changes = slices.DeleteFunc(w.plugin.changes, func(change platform.InputChange) bool { return change.Window == window })
	closed.Destroy()
	return nil
}

// Pump has the HostLoop pump the operating system's events, once it is
// started, then appends to changes what happened to the windows since the
// last Pump, and answers it.
func (w windowSystem) Pump(changes []platform.InputChange) []platform.InputChange {
	if w.plugin.loop != nil {
		w.plugin.loop.PumpEvents()
	}
	changes = append(changes, w.plugin.changes...)
	w.plugin.changes = w.plugin.changes[:0]
	return changes
}

// requestClose reports the user's close of window from the next Pump, and
// answers false, which keeps it open: closing it is the application's.
func (w windowSystem) requestClose(window platform.Window) bool {
	w.plugin.changes = append(w.plugin.changes, platform.InputChange{Kind: platform.InputChangeCloseRequested, Window: window})
	return false
}

// reportSizes reports from the next Pump a change of kind for window, at
// opened's size, framebuffer size and scale, the system's own for the screen
// it is on.
func (w windowSystem) reportSizes(kind platform.InputChangeKind, window platform.Window, opened *host.Window) {
	width, height := opened.Size()
	framebufferWidth, framebufferHeight := opened.PhysicalSize()
	w.plugin.changes = append(w.plugin.changes, platform.InputChange{
		Kind:            kind,
		Window:          window,
		Size:            platform.Size{Width: width, Height: height},
		FramebufferSize: platform.Size{Width: framebufferWidth, Height: framebufferHeight},
		Scale:           float32(opened.ScaleFactor()),
	})
}

// reportFocus reports from the next Pump window's gain of the focus, or its
// loss when focused is false, which forgets the keys held: platform releases
// them, so the next press of one is no repeat.
func (w windowSystem) reportFocus(window platform.Window, focused bool) {
	w.plugin.changes = append(w.plugin.changes, platform.InputChange{Kind: platform.InputChangeFocus, Window: window, Focused: focused})
	if !focused {
		w.plugin.heldKeys.Clear()
	}
}

// reportClosed reports from the next Pump the window gogpu closed by itself,
// which closed names. A window Close destroyed is forgotten first, and so
// not reported.
func (w windowSystem) reportClosed(closed host.WindowID) {
	for window, opened := range w.plugin.windows {
		if opened.ID() == closed {
			delete(w.plugin.windows, window)
			w.plugin.changes = append(w.plugin.changes, platform.InputChange{Kind: platform.InputChangeWindowClosed, Window: window})
			return
		}
	}
}
