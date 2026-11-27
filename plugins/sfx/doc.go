// Package sfx gives a game sound: clips played as voices, grouped on buses,
// positioned in the world and heard from one listener, with all of the
// arithmetic computed in sfx and none of it below. It is an interface plugin,
// as gfx is: a backend binds its socket and mixes what it is handed.
//
// A game records operations on the Queue from its own tasks, under
// kernel.Write[sfx.Queue]: a Play names a clip by its path in platform's FS
// and hands back the voice's handle at once. It schedules Tasks.Flush on each
// update beat, after its recorders, which applies what was recorded, advances
// every playhead and publishes a VoiceEndedEvent for each voice that ended.
// It groups voices on buses it declares, under Master, moving each bus's
// volume with SetBus and reading it back from BusVolumes. It reads the live
// voices from LiveVoices. It preloads a clip with Preload, so a loading screen
// pays its read, asks what it is with ClipTable.Info, which never starts a
// load, and gets its memory back with Release or ReleaseAll, which end the
// voices on it with ReasonReleased. No operation returns an error: a clip that cannot
// be read or prepared is logged once under its path, as an ErrClipUnreadable
// or an ErrClipFailed, and its voices end with ReasonFailed. Package sfxtest
// runs sfx in a real kernel over the headless backend, for tests.
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
