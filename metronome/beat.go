package metronome

import "time"

// Beat is the header every track event embeds. metronome stamps it as it
// publishes a beat; on an event published by hand, every field is zero.
type Beat struct {
	// Tick is this beat's number on its own track, from 1, never reset.
	Tick uint64
	// Delta is the time this beat stands for: on a fixed-step track, the step;
	// on a variable track, the clock time since the previous beat, raw and
	// unsmoothed, clamped to Settings.MaxDelta when set.
	Delta time.Duration
	// Lost is the time thrown away just before this beat, by steps dropped
	// past the debt cap or by the MaxDelta clamp; 0 when on pace. On a
	// fixed-step track it is a whole multiple of the step.
	Lost time.Duration
	// TimeFromStart is, on a fixed-step track, the tick's deadline from the
	// clock's epoch: Tick × step plus the Lost so far; on a variable track,
	// the publish time from the epoch. On every track it is the sum of
	// Delta + Lost over the track's beats.
	TimeFromStart time.Duration
	// Alpha is, in [0,1), how far past InterpolatedTick this beat falls, for
	// blending that tick's predecessor into it: the clock time at this beat's
	// publish less the tick's deadline, over the reference track's step. While
	// the
	// reference track is in debt it is the largest float32 below 1, so the
	// beat shows the newest state. Zero on a track with no reference track.
	Alpha float32
	// InterpolatedTick is the last tick of the reference track published
	// before this beat, whose state this beat's tasks read: locks order them
	// after the writes its publication scheduled. Zero on a track with no
	// reference track, and before the reference track's first tick.
	InterpolatedTick uint64
}

// UpdateEvent is the one event metronome ships, for an update track. Any
// other track event is the application's: a struct embedding Beat, whatever
// else it holds being its payload.
type UpdateEvent struct {
	Beat
}

// beater is what AddTrack needs of a track event: its embedded Beat, which
// metronome stamps through the promoted method.
type beater interface {
	stamp(beat Beat)
}

// stamp sets the header to beat.
func (b *Beat) stamp(beat Beat) { *b = beat }
