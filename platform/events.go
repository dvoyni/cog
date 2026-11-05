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
