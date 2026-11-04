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
// A variable track, from Variable, has no step: each beat's Delta is the time
// since the previous beat, clamped to Settings.MaxDelta after a stall, the
// time clamped away being its Lost. Settings.MinInterval is the least time
// between two beats; with none, the track's timer is reset to now and the
// track beats as soon as its in-flight limit allows.
//
// A beat is in flight until its publication's out dependency finishes: the
// job the last handler hands back. A track starts a new beat only while fewer
// than Settings.MaxInFlight of its beats are in flight; FixedStep keeps one,
// so a handler holding its out dependency open stalls the track, which
// catches up within its debt cap once released.
//
// The draw modes of a render track are combinations of a variable track's
// settings, not modes of their own:
//
//   - vsync: MaxInFlight 2 and no MinInterval. A FIFO present blocks, the
//     beat's out dependency waits on it, and the display paces the track.
//   - limit(fps): MinInterval 1/fps, plus a MaxInFlight.
//   - uncapped: a MaxInFlight alone.
//
// A handler that schedules a pinned job returns it in its out dependency,
// ex.Join(in, job), so the in-flight limit sees it. Otherwise each beat
// schedules another such job, and they pile up behind a blocked main thread.
//
// metronome reads time only through its Clock. The default, a WallClock,
// reads monotonic time with the epoch at Start; WithClock gives another. A
// ManualClock moves only when told: Advance walks every track deadline by
// deadline, letting each beat due publish and settle, and Jump moves time at
// metronome reads time only through its Clock. The default, a WallClock,
// reads monotonic time with the epoch at Start; WithClock gives another. A
// ManualClock moves only when told: Advance walks every track deadline by
// deadline, letting each beat due publish and settle, and Jump moves time at
// once, as a stall would, so debt builds. A variable track beats at most once
// per Advance or Jump, at its target time. Plugin.Wait returns once every
// track has settled: no beat in flight and no debt.
//
// metronome knows nothing of rendering, windows, input or the ECS.
//
// See docs/design.md.
package metronome
