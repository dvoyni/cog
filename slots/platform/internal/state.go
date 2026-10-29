package internal

import (
	"time"

	"github.com/dvoyni/cog/kernel"
)

// storeFailed keys the one report of an upload that failed, so a storefront
// that stays unreachable is said once rather than every StoreInterval.
type storeFailed struct{}

// refusedAchievement and refusedStat key the one report of a name the
// storefront does not know, which is a name missing from the storefront's own
// configuration and stays missing for the whole run.
type (
	refusedAchievement string
	refusedStat        string
)

// state is platform's one resource, and the write lock on it is what
// serializes every call into the Backend. It is unexported because nothing
// outside platform reads it: whether an upload is pending is platform's
// business, not the game's.
type state struct {
	// dirty reports changes the Backend has been told and has not uploaded.
	dirty bool
	// lastStore is when the last upload was attempted, successful or not, so a
	// failing storefront is retried at the interval and not every frame.
	lastStore time.Time
	// closed reports that the Backend was closed, after which nothing reaches
	// it.
	closed bool
}

// store uploads the pending changes. A failure leaves them pending, to go up
// with the next attempt.
func (s *state) store(k kernel.Kernel, backend Backend) {
	s.lastStore = time.Now()
	if err := backend.Store(); err != nil {
		k.ReportErrorOnce(storeFailed{}, err)
		return
	}
	s.dirty = false
}
