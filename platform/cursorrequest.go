package platform

// CursorRequest is what the application wants a window's cursor to be, beside
// its entry in the window request: WindowManager.SetCursorShape,
// SetCursorHidden and SetCursorMode set it, and the pump applies it through
// WindowsSocket.SetCursor. The zero request is the default shape, visible, in
// CursorModeNormal. The pending cursor position is no part of it:
// WindowManager.SetCursorPosition sets that.
type CursorRequest struct {
	Shape CursorShape
	// Hidden hides the cursor over the window, keeping Shape for when it
	// shows again.
	Hidden bool
	Mode   CursorMode
}

// CursorShape is what the cursor looks like over a window.
type CursorShape uint8

const (
	// CursorShapeDefault is the arrow.
	CursorShapeDefault CursorShape = iota
	// CursorShapePointer is the hand over something clickable.
	CursorShapePointer
	// CursorShapeText is the I-beam over text.
	CursorShapeText
	CursorShapeCrosshair
	// CursorShapeMove is the four arrows over something movable.
	CursorShapeMove
	CursorShapeResizeNorthSouth
	CursorShapeResizeEastWest
	CursorShapeResizeNorthwestSoutheast
	CursorShapeResizeNortheastSouthwest
	CursorShapeNotAllowed
	CursorShapeWait
)

// CursorMode is how the cursor is held to a window.
type CursorMode uint8

const (
	// CursorModeNormal leaves the cursor visible and free.
	CursorModeNormal CursorMode = iota
	// CursorModeConfined keeps the cursor visible, inside the window.
	CursorModeConfined
	// CursorModeLocked hides the cursor and holds it in place: the window's
	// pointer position stays frozen, and PointerMoveEvent's Delta carries the
	// mouse's motion.
	CursorModeLocked
)
