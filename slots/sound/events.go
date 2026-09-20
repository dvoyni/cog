package sound

import "github.com/dvoyni/cog/slots/sound/internal/types"

// Reason says why a Voice ended.
type Reason = types.Reason

const (
	// ReasonFinished is the Clip reaching its end while not looping.
	ReasonFinished Reason = types.ReasonFinished
	// ReasonStopped is Stop, or a StopBus covering it.
	ReasonStopped Reason = types.ReasonStopped
	// ReasonStolen is the cap, whether the Voice played for a second or never
	// sounded.
	ReasonStolen Reason = types.ReasonStolen
	// ReasonFailed is a Clip that could not be read or prepared. It arrives one
	// or more ticks after the Play, never in the same tick.
	ReasonFailed Reason = types.ReasonFailed
	// ReasonReleased is the Clip the Voice was playing being released.
	ReasonReleased Reason = types.ReasonReleased
)

// VoiceEndedEvent is published for every ending, so a subscriber never has to
// tell "it ended" from "I ended it" to keep its own bookkeeping straight.
// Dialogue sequencing and playlists land on it.
//
// There is no VoiceStartedEvent, and the reason is what an event is for: an
// event exists to tell a game something it did not do. Finishing, being stolen,
// failing and being released are none of them the game's doing; a start is -
// the game recorded the Play synchronously and already knows.
//
// A stop is published on the tick it was recorded and not when the Adapter's
// declick ramp finishes, so the event precedes the silence by a few
// milliseconds. It never meant "the speaker is now quiet".
//
// Shutdown publishes nothing: an event nobody can receive is not an ending
// worth modelling.
type VoiceEndedEvent struct {
	Voice  Voice
	Reason Reason
}
