package platform

// InputChangeKind says what an InputChange reports.
type InputChangeKind uint8

const (
	// InputChangeWindowOpened reports that a window's operating-system window
	// exists, with its sizes, its scale and its native handle. WindowsSocket
	// reports it from the Pump after Open.
	InputChangeWindowOpened InputChangeKind = iota + 1
	// InputChangeWindowClosed reports that a window closed by itself, without
	// its entry being removed. WindowsSocket reports it from the Pump.
	InputChangeWindowClosed
	// InputChangeCloseRequested reports that the user asked to close a window.
	InputChangeCloseRequested
	// InputChangeKey reports a key pressed, repeated or released in the
	// focused window: Key, Pressed, Repeat, Modifiers, and Position for a
	// mouse button.
	InputChangeKey
	// InputChangeText reports one character typed in the focused window:
	// Char.
	InputChangeText
	// InputChangeFocus reports that a window gained or lost the keyboard
	// focus: Focused.
	InputChangeFocus
	// InputChangePointerMove reports the pointer at Position in the window
	// under it, or in the one that captured it during a drag. platform takes
	// the motion from the positions.
	InputChangePointerMove
	// InputChangeScroll reports a scroll over the window under the pointer:
	// ScrollDelta and ScrollLines.
	InputChangeScroll
	// InputChangePointerEnter reports the pointer come over a window, at
	// Position.
	InputChangePointerEnter
	// InputChangePointerLeave reports the pointer gone from a window.
	InputChangePointerLeave
	// InputChangeResize reports a window's new size, framebuffer size and
	// scale: Size, FramebufferSize and Scale. WindowsSocket reports it from the
	// Pump.
	InputChangeResize
)

// InputChange is one raw change the WindowsSocket or the InputSourceSocket
// reports, which ProcessEvents folds into the state and publishes as an
// event. It is pointer-free. Every change names a Window the window
// implementation was asked to open and has not reported closed, and a key
// change one of the Keys; any other panics.
type InputChange struct {
	Kind   InputChangeKind
	Window Window
	// Size, FramebufferSize, Scale and Native are the opened window's, for
	// InputChangeWindowOpened; the first three are the resized window's, for
	// InputChangeResize.
	Size            Size
	FramebufferSize Size
	Scale           float32
	Native          NativeHandle
	// Key, Pressed, Repeat, Modifiers and Position are an InputChangeKey's,
	// as KeyEvent carries them. Position is also an InputChangePointerMove's
	// and an InputChangePointerEnter's, in window space.
	Key       Key
	Pressed   bool
	Repeat    bool
	Modifiers Modifiers
	Position  Point
	// ScrollDelta and ScrollLines are an InputChangeScroll's, as
	// ScrollEvent's Delta and Lines.
	ScrollDelta Point
	ScrollLines bool
	// motion is an InputChangePointerMove's, which platform takes from the
	// positions and sums as it coalesces.
	motion Point
	// Char is an InputChangeText's character.
	Char rune
	// Focused is an InputChangeFocus's: false when the focus is lost.
	Focused bool
}
