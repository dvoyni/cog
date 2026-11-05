package platform

// Window names one window: an opaque, pointer-free handle, given by
// WindowManager.Open from a counter that is never reused. A Window kept past
// its window's close names nothing, never a window opened later. The zero
// Window names none, but for the getters of the window state and the input
// state, where it reads the only window the window state knows, and panics
// unless it knows exactly one: a game of one window needs no handle.
type Window struct {
	id uint64
}

// Size is a width and a height: in window space for a window's size, in
// pixels for its framebuffer's.
type Size struct {
	Width, Height int
}

// Point is a pair of coordinates, X rightward and Y downward: a position in
// window space, a window's logical points from its top left corner, or the
// difference between two, or the amounts of a scroll.
type Point struct {
	X, Y float32
}

// WindowEntry is what the application wants one window to be: its entry in
// the WindowRequest.
type WindowEntry struct {
	Title string
	// Size is the window's size in window space.
	Size       Size
	Fullscreen bool
	// Hidden keeps the window off screen. The zero entry is a visible window.
	Hidden bool
}

// NativeHandle is what the operating system names a window by, which gfx
// creates its surface from: the display or instance, and the window.
type NativeHandle struct {
	Display, Window uintptr
}
