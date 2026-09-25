package types

// ViewportMode selects how the logical viewport responds to window aspect
// changes. ViewportWindow uses the window dimensions directly; fixed modes keep
// one dimension constant; Fit shows the full desired rectangle, while Cover
// fills the viewport from it.
type ViewportMode uint8

const (
	ViewportWindow ViewportMode = iota
	ViewportFixedWidth
	ViewportFixedHeight
	ViewportFit
	ViewportCover
)
