package canvas

// ErrDrawsBusy reports a draw-snapshot arm made while one is already live. It
// is refused rather than queued, for the reason a second frame snapshot is:
// the window is one tick and the retry is one call.
//
// It is deliberately its own kind. A capture and each package's snapshot are
// separate slots and may be in flight together - refusing across kinds would
// destroy the one thing arming them together is for, which is describing a
// single tick.
type ErrDrawsBusy struct{}

func (ErrDrawsBusy) Error() string { return "canvas: a draw snapshot is already in flight" }

// ErrDrawsAbandoned reports a draw snapshot the engine stopped before a tick
// recorded anything. It travels the channel a result would have used, so that
// a waiter learns the answer rather than sitting until its own deadline.
type ErrDrawsAbandoned struct{}

func (ErrDrawsAbandoned) Error() string {
	return "canvas: the engine stopped before a tick was recorded"
}

// ErrDrawsUnknownKind reports a kind filter naming something canvas does not
// record. It is raised before anything is armed, because a typo that costs a
// tick and comes back as an empty array reads as a game that drew nothing.
type ErrDrawsUnknownKind struct{ Kind string }

func (e ErrDrawsUnknownKind) Error() string {
	return "canvas: " + e.Kind + " is not a draw kind; the kinds are sprite, text and triangles"
}
