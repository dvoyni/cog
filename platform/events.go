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
