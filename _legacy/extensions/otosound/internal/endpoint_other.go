//go:build !windows && !js

package internal

// Everywhere that is not Windows, oto answers the question itself.
//
// Windows is the only driver in oto v3.5.0 that substitutes silence for an
// absent device, and it is worth saying what the other two do rather than
// assuming they do the same, because the answer is what decides that this file
// is a constant:
//
//   - macOS and iOS (driver_darwin.go). There is no null anything. The context
//     builds an AudioQueue on its own thread; a failure to build one is joined
//     into c.err and comes straight back out of Err, which is what otosound
//     polls and what play checks before it takes a player. A machine with no
//     output device fails at AudioQueueNewOutput or at the first
//     AudioQueueStart, and either way it is reported.
//
//     The one thing darwin has that resembles the Windows hole is its deferred
//     start: an AudioQueueStart that fails because the audio session cannot be
//     activated - the app is in the background, another app owns the session,
//     media services are restarting - is retried on a backoff instead of being
//     recorded, and the context stays silent and error-free in the meantime.
//     That is a session being unavailable for a while and not a device being
//     absent, so it belongs on the loss side of this Adapter's line, where
//     silence is the contract; a probe here would misreport it as a machine
//     with no sound card.
//
//   - Linux and the other Unixes (driver_unix.go). PulseAudio first, ALSA as
//     the fallback where that file is built, and when both fail their two
//     errors are joined into c.err. There is no third branch: no server and no
//     card is exactly the case that reaches the join, so Err reports it and the
//     Adapter says ErrDeviceUnavailable on the strength of the open alone.
//
// So on both, an open that succeeds means a device, and asking again would only
// add a way to be wrong.

// noOutputDevice reports a machine with no audio output device at all, which on
// these platforms is never: the answer oto's own open gives is trustworthy, and
// the Adapter already reports it.
func noOutputDevice() error { return nil }
