package ui

import "strconv"

// ErrLayoutBusy reports a layout-snapshot arm made while one is already live.
// It is refused rather than queued, for the reason a second draw or frame
// snapshot is: the window is one tick and the retry is one call.
//
// It is deliberately its own kind. A capture and each package's snapshot are
// separate slots and may be in flight together - refusing across kinds would
// destroy the one thing arming them together is for, which is describing a
// single tick.
type ErrLayoutBusy struct{}

func (ErrLayoutBusy) Error() string { return "ui: a layout snapshot is already in flight" }

// ErrLayoutAbandoned reports a layout snapshot the engine stopped before a
// tick processed anything. It travels the channel a result would have used, so
// that a waiter learns the answer rather than sitting until its own deadline.
type ErrLayoutAbandoned struct{}

func (ErrLayoutAbandoned) Error() string {
	return "ui: the engine stopped before a tick was processed"
}

// ErrLayoutNoSuchElement reports a subtree filter naming an index the tick's
// tree does not have. It can only be raised inside the tick - the tree is
// declared afresh every tick and nothing outside it knows how large it is -
// and it is raised rather than answered with an empty array, because an empty
// array reads as a subtree that laid out nothing.
type ErrLayoutNoSuchElement struct {
	Index, Count int
}

func (e ErrLayoutNoSuchElement) Error() string {
	if e.Count == 0 {
		return "ui: element " + strconv.Itoa(e.Index) +
			" is not in this tick's tree, which declared no elements at all"
	}
	return "ui: element " + strconv.Itoa(e.Index) +
		" is not in this tick's tree, which has " + strconv.Itoa(e.Count) +
		" elements indexed 0 to " + strconv.Itoa(e.Count-1)
}
