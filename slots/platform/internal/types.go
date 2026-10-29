package internal

// TextField is where on the window the player is typing, in pixels from the
// window's top-left corner. A storefront's on-screen keyboard opens where it
// does not cover it.
type TextField struct {
	X, Y, Width, Height int
}
