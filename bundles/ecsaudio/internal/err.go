package internal

import "fmt"

// ErrManyListeners reports more than one Entity carrying the Listener Tag. It
// is reported once for the engine's life, because a condition true every tick
// is worth saying once and is noise at the frame rate.
//
// It is a report and never a refusal: the world is still heard from the lowest
// of them, and from the same one every tick. An arbitrary pick that changed with
// iteration order would be a sound bug nobody could reproduce.
type ErrManyListeners struct{ Count int }

func (e ErrManyListeners) Error() string {
	return fmt.Sprintf("ecsaudio: %d Entities carry the Listener Tag; the lowest one is heard from", e.Count)
}
