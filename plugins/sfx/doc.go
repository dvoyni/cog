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
// voices from LiveVoices, and the device from DeviceView: a device that is
// absent, not ready yet or lost reads not ready, a fact rather than an error,
// and every playhead advances whether or not anyone can hear it. It preloads
// a clip with Preload, so a loading screen pays its read, asks what it is
// with ClipTable.Info, which never starts a load, and gets its memory back
// with Release or ReleaseAll, which end the voices on it with ReasonReleased.
// At most Config.MaxVoices voices exist at once: a play arriving at a full
// table steals from the voice in the lowest VoiceOptions.Priority band the
// least audible, ties going to the oldest, or is itself the one that loses,
// either ending with ReasonStolen, so music put a band up survives any crowd
// of footsteps.
// No operation returns an error: a clip that cannot be read or prepared is
// logged once under its path, as an ErrClipUnreadable or an ErrClipFailed,
// and its voices end with ReasonFailed. Package sfxtest runs sfx in a real
// kernel over the headless backend, for tests.
//
// A voice given a Position in VoiceOptions is positional for the rest of its
// life, heard from the one listener the game places with Queue.SetListener
// and reads back from Listener: quieter with distance by its Falloff, off
// its axis by its Cone when it has an Orientation, and panned by W3C's
// equal-power equations, which sfx computes. A voice with no position is
// heard centred. The axes are right-handed, forward -Z, up +Y and right +X,
// and the units are the game's: set Falloff.Ref to the world's scale, since
// W3C's default of 1 makes a source 10 units away -20 dB. A stereo clip is
// panned, not mixed down to mono: one channel passes at unity and the other
// bleeds into it, so a stereo clip panned hard right is its left channel
// folded into its right ear. Whether a positional clip should be mono is the
// game's judgement.
//
// A 2D game passes m.Vec3{X: x, Y: y} and rotates its listener once, so a
// sprite's Y is front and back rather than nothing:
//
//	queue.SetListener(sfx.ListenerOptions{Orientation: m.Some(m.QuatRotationX(-math.Pi / 2))})
//
// and turns a directional emitter θ in-plane with the same rotation,
// m.QuatRotationZ(θ).Mul(m.QuatRotationX(-math.Pi / 2)), facing up the
// screen at θ of 0.
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
