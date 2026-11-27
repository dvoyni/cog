// Package sfx gives a game sound: clips played as voices, grouped on buses,
// positioned in the world and heard from one listener, with all of the
// arithmetic computed in sfx and none of it below. It is an interface plugin,
// as gfx is: a backend binds its socket and mixes what it is handed.
//
// BackendSocket is the seam. A backend is told how many slots exist once,
// receives one FlushChanges per flush, multiplies a gain matrix it did not
// compute, prepares the clips whose bytes it is handed, and answers the
// device. It knows nothing of handles, buses, priorities, positions, the cap
// or the equations. Package headless holds the backend that plays nothing,
// for tests and for runs no one hears.
//
// See docs/design.md.
package sfx
