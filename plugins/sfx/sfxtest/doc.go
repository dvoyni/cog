// Package sfxtest runs sfx in a real kernel over the headless backend, with
// no device, for sfx's own tests and those of a game's sound.
//
// A test makes a Harness, spawns it with sfx's config and its own plugins,
// which are listed after sfx, mounts the bytes of the clips it plays, records
// and steps:
//
//	h := sfxtest.NewHarness(t)
//	h.Spawn(nil)
//	h.Mount("shot.ogg", shot)
//	var voice sfx.Voice
//	h.Record(func(queue sfx.Queue) { voice = queue.Play("shot.ogg", 0, sfx.VoiceOptions{}) })
//	h.Step()
//	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener) {
//		entry, live := voices.Info(voice)
//		...
//	})
//
// Each Step is one beat of a fixed-step track, StepInterval long, on a
// metronome.ManualClock: the harness's handler of it schedules the records
// queued since, then sfx's Tasks.Flush with the beat's Delta plus its Lost, as
// a game schedules it. Endings answers every VoiceEndedEvent published so
// far, in order, and Reports every error the kernel logged.
//
// The harness is strict: an error the kernel logs fails the test, checked
// after each Step and when the test ends, unless a match the test gave
// ExpectError answers true of it, such as a clip's failure it causes on
// purpose. UseBackend, before Spawn, binds a backend of the test's own in
// place of the headless one, for what the headless backend never does: a
// prepare answering done=false, or a device that is not ready.
//
// A test asserts against sfx's flush-computed state and its events, never
// against samples and never below the seam: above it, on a fixed step,
// everything is deterministic.
package sfxtest
