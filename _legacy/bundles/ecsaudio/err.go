package ecsaudio

import "github.com/dvoyni/cog/bundles/ecsaudio/internal"

// ErrManyListeners reports more than one Entity carrying the Listener Tag. It
// is reported once for the engine's life, because a condition true every tick
// is worth saying once and is noise at the frame rate.
//
// It is a report and never a refusal: the world is still heard from the lowest
// of them, and from the same one every tick. An arbitrary pick that changed with
// iteration order would be a sound bug nobody could reproduce.
type ErrManyListeners = internal.ErrManyListeners
