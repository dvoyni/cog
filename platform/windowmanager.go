package platform

import (
	"fmt"
	"slices"
)

// WindowManager holds every window: what the application wants it to be, its
// entry in the window request, and what it currently is, its window state. It
// is a resource, as a *WindowManager.
//
// A job opens, changes and closes windows under the write lock. Nothing
// reaches the operating system until the next ProcessEvents run, which applies
// what changed since its last, so several changes in one frame cost one call.
// The window state is written only by ProcessEvents; any job reads it under
// the read lock. Its getters given the zero Window read the only window the
// state knows, and panic unless it knows exactly one, so a game of one window
// needs no handle.
type WindowManager struct {
	// lastID is the id of the last Window Open gave.
	lastID uint64
	// entries holds every window's entry in the request, in the order the
	// windows were opened.
	entries []requestEntry
	// states holds every window known to the state, in the order they were
	// opened: from the run that asks the implementation to open it until the
	// run that sees it closed.
	states []windowStateEntry
}

// requestEntry is one window's entry in the request.
type requestEntry struct {
	window Window
	entry  WindowEntry
	cursor CursorRequest
	// cursorPosition is the pending cursor position, when
	// cursorPositionPending.
	cursorPosition        Point
	cursorPositionPending bool
}

// windowStateEntry is what one window currently is.
type windowStateEntry struct {
	window          Window
	open            bool
	size            Size
	framebufferSize Size
	scale           float32
	native          NativeHandle
	focused         bool
	// cursorMode is the effective cursor mode.
	cursorMode CursorMode
}

// Open adds entry to the request and answers its Window at once. The window
// opens on the next ProcessEvents run.
func (m *WindowManager) Open(entry WindowEntry) Window {
	m.lastID++
	window := Window{id: m.lastID}
	m.entries = append(m.entries, requestEntry{window: window, entry: entry})
	return window
}

// Entry answers window's entry in the request, and false if window names no
// entry.
func (m *WindowManager) Entry(window Window) (WindowEntry, bool) {
	index := m.findEntry(window)
	if index < 0 {
		return WindowEntry{}, false
	}
	return m.entries[index].entry, true
}

// Change replaces window's whole entry, and answers false, changing nothing,
// if window names no entry.
func (m *WindowManager) Change(window Window, entry WindowEntry) bool {
	index := m.findEntry(window)
	if index < 0 {
		return false
	}
	m.entries[index].entry = entry
	return true
}

// SetTitle requests title for window, and answers false if window names no
// entry.
func (m *WindowManager) SetTitle(window Window, title string) bool {
	return m.edit(window, func(entry *WindowEntry) { entry.Title = title })
}

// SetSize requests size, in window space, for window, and answers false if
// window names no entry. Size answers the size the window has, not this one.
func (m *WindowManager) SetSize(window Window, size Size) bool {
	return m.edit(window, func(entry *WindowEntry) { entry.Size = size })
}

// SetFullscreen requests window fullscreen or not, and answers false if window
// names no entry.
func (m *WindowManager) SetFullscreen(window Window, fullscreen bool) bool {
	return m.edit(window, func(entry *WindowEntry) { entry.Fullscreen = fullscreen })
}

// SetHidden requests window hidden or shown, and answers false if window
// names no entry.
func (m *WindowManager) SetHidden(window Window, hidden bool) bool {
	return m.edit(window, func(entry *WindowEntry) { entry.Hidden = hidden })
}

// SetCursorShape requests shape for window's cursor, and answers false if
// window names no entry.
func (m *WindowManager) SetCursorShape(window Window, shape CursorShape) bool {
	return m.editCursor(window, func(cursor *CursorRequest) { cursor.Shape = shape })
}

// SetCursorHidden requests window's cursor hidden or shown, and answers false
// if window names no entry. Hiding it keeps its shape.
func (m *WindowManager) SetCursorHidden(window Window, hidden bool) bool {
	return m.editCursor(window, func(cursor *CursorRequest) { cursor.Hidden = hidden })
}

// SetCursorMode requests mode for window's cursor, and answers false if
// window names no entry. EffectiveCursorMode answers the mode in force.
func (m *WindowManager) SetCursorMode(window Window, mode CursorMode) bool {
	return m.editCursor(window, func(cursor *CursorRequest) { cursor.Mode = mode })
}

// SetCursorPosition moves window's cursor to position, in window space, on the
// next ProcessEvents run, and answers false if window names no entry. Several
// set before one run collapse to the last. The move produces no
// PointerMoveEvent; the input state's pointer position becomes position. The
// run drops it while the requested cursor mode is CursorModeLocked.
func (m *WindowManager) SetCursorPosition(window Window, position Point) bool {
	index := m.findEntry(window)
	if index < 0 {
		return false
	}
	m.entries[index].cursorPosition = position
	m.entries[index].cursorPositionPending = true
	return true
}

// Close removes window's entry from the request; the window closes on the
// next ProcessEvents run. A window that names no entry is left alone.
func (m *WindowManager) Close(window Window) {
	if index := m.findEntry(window); index >= 0 {
		m.entries = slices.Delete(m.entries, index, index+1)
	}
}

// IsOpen answers whether window's operating-system window exists: false
// before the ProcessEvents run that opens it, and after the one that closes
// it.
func (m *WindowManager) IsOpen(window Window) bool { return m.state(window).open }

// Size answers window's current size in window space, or the zero Size for a
// window not open.
func (m *WindowManager) Size(window Window) Size { return m.state(window).size }

// FramebufferSize answers the size of window's framebuffer in pixels: its
// size times its scale.
func (m *WindowManager) FramebufferSize(window Window) Size {
	return m.state(window).framebufferSize
}

// Scale answers how many framebuffer pixels one window-space point of window
// covers.
func (m *WindowManager) Scale(window Window) float32 { return m.state(window).scale }

// Native answers the native handle of window, valid while it is open.
func (m *WindowManager) Native(window Window) NativeHandle { return m.state(window).native }

// IsFocused answers whether window has the keyboard focus.
func (m *WindowManager) IsFocused(window Window) bool { return m.state(window).focused }

// EffectiveCursorMode answers the cursor mode in force in window: its
// requested one, but CursorModeNormal while the window lacks the focus, or
// another where the platform lacks the requested mode. ProcessEvents
// re-applies the requested mode when the focus returns.
func (m *WindowManager) EffectiveCursorMode(window Window) CursorMode {
	return m.state(window).cursorMode
}

// edit changes window's entry through change, and answers false, changing
// nothing, if window names no entry.
func (m *WindowManager) edit(window Window, change func(entry *WindowEntry)) bool {
	index := m.findEntry(window)
	if index < 0 {
		return false
	}
	change(&m.entries[index].entry)
	return true
}

// editCursor changes window's cursor request through change, and answers
// false, changing nothing, if window names no entry.
func (m *WindowManager) editCursor(window Window, change func(cursor *CursorRequest)) bool {
	index := m.findEntry(window)
	if index < 0 {
		return false
	}
	change(&m.entries[index].cursor)
	return true
}

// findEntry answers the index of window's entry, or -1 if it has none.
func (m *WindowManager) findEntry(window Window) int {
	return slices.IndexFunc(m.entries, func(e requestEntry) bool { return e.window == window })
}

// state answers what window currently is, or the zero entry if the state does
// not know it. The zero Window is the only window the state knows; it panics
// unless the state knows exactly one.
func (m *WindowManager) state(window Window) windowStateEntry {
	if window == (Window{}) {
		if len(m.states) != 1 {
			panic(fmt.Sprintf("platform: the zero Window reads the only window, but %d are known", len(m.states)))
		}
		return m.states[0]
	}
	index := m.findState(window)
	if index < 0 {
		return windowStateEntry{}
	}
	return m.states[index]
}

// findState answers the index of window in the state, or -1 if it does not
// know it. The zero Window is never known.
func (m *WindowManager) findState(window Window) int {
	if window == (Window{}) {
		return -1
	}
	return slices.IndexFunc(m.states, func(e windowStateEntry) bool { return e.window == window })
}

// addState makes window known to the state, not open yet.
func (m *WindowManager) addState(window Window) {
	m.states = append(m.states, windowStateEntry{window: window})
}

// removeState forgets window in the state.
func (m *WindowManager) removeState(window Window) {
	if index := m.findState(window); index >= 0 {
		m.states = slices.Delete(m.states, index, index+1)
	}
}
