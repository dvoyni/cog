package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// ErrDrawsBusy reports a draw-snapshot arm made while one is already live. It
// is refused rather than queued, for the reason a second frame snapshot is:
// the window is one tick and the retry is one call.
//
// It is deliberately its own kind. A capture and each package's snapshot are
// separate slots and may be in flight together - refusing across kinds would
// destroy the one thing arming them together is for, which is describing a
// single tick.
type ErrDrawsBusy = internal.ErrDrawsBusy

// ErrDrawsAbandoned reports a draw snapshot the engine stopped before a tick
// recorded anything. It travels the channel a result would have used, so that
// a waiter learns the answer rather than sitting until its own deadline.
type ErrDrawsAbandoned = internal.ErrDrawsAbandoned

// ErrDrawsUnknownKind reports a kind filter naming something canvas does not
// record. It is raised before anything is armed, because a typo that costs a
// tick and comes back as an empty array reads as a game that drew nothing.
type ErrDrawsUnknownKind = internal.ErrDrawsUnknownKind
