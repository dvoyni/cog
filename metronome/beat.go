package metronome

import "time"

// Beat is the header every track event embeds. metronome stamps it as it
// publishes a beat; on an event published by hand, every field is zero.
type Beat struct {
	// Tick is this beat's number on its own track, from 1, never reset.
	Tick uint64
	// Delta is the time this beat stands for: on a fixed-step track, the step.
	Delta time.Duration
	// TimeFromStart is, on a fixed-step track, the tick's deadline from the
	// clock's epoch: Tick × step.
	TimeFromStart time.Duration
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
