package internal

// LayoutSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type LayoutSnapshot struct {
	Layout LayoutView
	// Tick is app.UpdateEvent.Tick of the tick the snapshot was taken in. It
	// travels with the snapshot rather than being asked for afterwards,
	// because only the tick itself knows which one it was.
	Tick int64
	Err  error
}
