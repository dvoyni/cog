package platform

// WindowOpenedEvent is published once Window's operating-system window exists
// and its native handle is valid.
type WindowOpenedEvent struct {
	Window Window
}

// WindowClosedEvent is published once Window's operating-system window is
// gone: its entry was removed, or it closed by itself. Window names nothing
// from then on.
type WindowClosedEvent struct {
	Window Window
}

// CloseRequestedEvent is published when the user asks to close Window. It
// closes nothing: the window closes once the application removes its entry.
type CloseRequestedEvent struct {
	Window Window
}

// ResizeEvent is published when Window's size, framebuffer size or scale
// changes, with all three as the window state now holds them.
type ResizeEvent struct {
	Window Window
	// Size is in window space.
	Size Size
	// FramebufferSize is in pixels: Size times Scale.
	FramebufferSize Size
	Scale           float32
}

// KeyEvent is published when a Key is pressed, repeats or is released, while
// Window has focus. Losing focus releases every held key, each with its own
// KeyEvent.
type KeyEvent struct {
	Window Window
	Key    Key
	// Pressed is false for a release.
	Pressed bool
	// Repeat is a press the operating system repeats while the key stays
	// held.
	Repeat    bool
	Modifiers Modifiers
	// Position is the pointer's, in Window's window space, for a mouse
	// button.
	Position Point
}

// TextEvent is published for one character the user typed in Window, after
// the layout, dead keys and the input method, in order with the KeyEvents.
// Text is not keys: a text field reads TextEvents, gameplay and shortcuts
// KeyEvents.
type TextEvent struct {
	Window Window
	Char   rune
}

// FocusEvent is published when Window gains or loses the keyboard focus.
type FocusEvent struct {
	Window Window
	// Focused is false when the focus is lost.
	Focused bool
}

// PointerMoveEvent is published once per ProcessEvents run for each Window
// the pointer moved in: the moves of the run coalesce into one, in the place
// of the last. Window is the one under the pointer, or the one that captured
// it during a drag, where Position may lie outside it.
type PointerMoveEvent struct {
	Window Window
	// Position is the pointer's last in the run, in Window's window space.
	Position Point
	// Delta is the run's moves summed, in window space.
	Delta Point
}

// ScrollEvent is published when the user scrolls over Window, the one under
// the pointer.
type ScrollEvent struct {
	Window Window
	// Delta is the amount scrolled: positive X scrolls right, positive Y
	// down.
	Delta Point
	// Lines is true when Delta counts lines, as a wheel's notches do, and
	// false when it counts window-space points, as a touchpad's do.
	Lines bool
}

// PointerEnterEvent is published when the pointer comes over Window.
type PointerEnterEvent struct {
	Window Window
}

// PointerLeaveEvent is published when the pointer leaves Window.
type PointerLeaveEvent struct {
	Window Window
}
