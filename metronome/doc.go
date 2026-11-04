// Package metronome paces the work a kernel does over time.
//
// An application declares tracks: a plugin listed after metronome calls
// AddTrack in its Init, naming the track's event type, which embeds Beat, and
// its Settings. Each track runs on its own goroutine from metronome's Start
// to its Stop and publishes its event as beats, so its handlers run there and
// a slow handler delays no other track.
//
//	metronome.AddTrack[metronome.UpdateEvent](configurator, metronome.FixedStep(60))
//
// A fixed-step track's tick n is due at the clock's epoch + n × step, never
// summed from sleeps. A beat stays in flight until its publication's out
// dependency finishes, and the track publishes its next beat only after
// that.
//
// metronome reads time only through its Clock. The default reads monotonic
// time with the epoch at Start; WithClock gives another. A ManualClock moves
// only when told: Advance walks every track deadline by deadline, letting
// each beat due publish and settle. Plugin.Wait returns once every track has
// settled.
//
// metronome knows nothing of rendering, windows, input or the ECS.
//
// See docs/design.md.
package metronome
