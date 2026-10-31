package internal

import "testing"

// Quit arrives from the engine's goroutine, not the one inside Run, so it may
// land before Run entered the platform loop or after it left. Both must do
// nothing. The nil App is what proves it: a Quit that reached through to the
// App would panic here, which is exactly the crash the guard prevents on a
// plugin whose Run has already returned.
func TestQuitOutsideThePlatformLoopDoesNothing(t *testing.T) {
	p := &plugin{}

	p.Quit() // before Run

	p.looping.Store(true)
	p.looping.Store(false)
	p.Quit() // after Run returned
}
