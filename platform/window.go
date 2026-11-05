package platform

// Window names one window: an opaque, pointer-free handle, given by
// WindowRequest.Open from a counter that is never reused. The zero Window
// names none, and a Window kept past its window's close names nothing, never
// a window opened later.
type Window struct {
	id uint64
}

// Size is a width and a height: in window space for a window's size, in
// pixels for its framebuffer's.
type Size struct {
	Width, Height int
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
