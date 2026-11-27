//go:build !windows && !js

package otosound

// probeOutputDevice answers nil: everywhere but Windows, an open that
// succeeds means a device, so there is nothing to ask before it.
//
// Windows is the only oto v3.5.1 driver that substitutes silence for an
// absent device. On macOS the context builds an AudioQueue on its own
// thread, and a failure to build one is joined into the context's error and
// comes straight out of Err, which starting the player checks. On Linux oto
// tries PulseAudio and falls back to ALSA, and when both fail it joins their
// errors: no server and no card is exactly the case that reaches the join.
// Asking again would only add a way to be wrong.
//
// darwin has a silent-but-ready state of another kind: an AudioQueueStart
// that fails because the audio session cannot be activated, the app in the
// background, another app owning the session, media services restarting,
// is retried by oto on a backoff rather than recorded, and the context stays
// quiet and error-free meanwhile. That is a session unavailable for a while,
// not a device absent, so it sits on the loss side, where silence is the
// contract; a probe here would misreport a backgrounded app as a machine
// with no sound card.
func probeOutputDevice() error { return nil }
