package platform

// InputChangeKind says what an InputChange reports.
type InputChangeKind uint8

const (
	// InputChangeWindowOpened reports that a window's operating-system window
	// exists, with its sizes, its scale and its native handle. Windows
	// reports it from the Pump after Open.
	InputChangeWindowOpened InputChangeKind = iota + 1
	// InputChangeWindowClosed reports that a window closed by itself, without
	// its entry being removed. Windows reports it from the Pump.
	InputChangeWindowClosed
	// InputChangeCloseRequested reports that the user asked to close a window.
	InputChangeCloseRequested
)

// InputChange is one raw change Windows or an InputSource reports, which
// ProcessEvents folds into the state and publishes as an event. It is
// pointer-free. Every change names a Window the Windows implementation was
// asked to open and has not reported closed; one naming any other window
// panics.
type InputChange struct {
	Kind   InputChangeKind
	Window Window
	// Size, FramebufferSize, Scale and Native are the opened window's, for
	// InputChangeWindowOpened.
	Size            Size
	FramebufferSize Size
	Scale           float32
	Native          NativeHandle
}
