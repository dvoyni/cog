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
// A fixed-step track's deadlines are computed from the clock's epoch, one
// step apart, never summed from sleeps. Being past the next deadline is debt:
// the steps owed and not yet published, capped by Settings.MaxDebt. Owed steps
// publish back-to-back; a step owed past the cap is dropped, gets no tick, and
// its time is the next beat's Lost, so TimeFromStart is always the sum of
// Delta + Lost over the track's beats.
//
// A beat is in flight until its publication's out dependency finishes: the
// job the last handler hands back. A track starts a new beat only while fewer
// than Settings.MaxInFlight of its beats are in flight; FixedStep keeps one,
// so a handler holding its out dependency open stalls the track, which
// catches up within its debt cap once released.
//
// A handler that schedules a pinned job returns it in its out dependency,
// ex.Join(in, job), so the in-flight limit sees it. Otherwise each beat
// schedules another such job, and they pile up behind a blocked main thread.
//
// metronome reads time only through its Clock, given with WithClock. A
// ManualClock moves only when told: Advance walks every track deadline by
// deadline, letting each beat due publish and settle, and Jump moves time at
// once, as a stall would, so debt builds. Plugin.Wait returns once every track
// has settled: no beat in flight and no debt.
//
// metronome knows nothing of rendering, windows, input or the ECS.
//
// See docs/design.md.
package metronome
