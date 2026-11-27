package sfx

// VoiceEndedEvent is published for every ending, so a subscriber never has to
// tell "it ended" from "I ended it" to keep its own bookkeeping straight.
// Dialogue sequencing and playlists land on it. It carries no clip: a game
// holds the handle it played with and already knows.
//
// It is published from inside the flush, after the flush's changes are
// emitted, one per ending in the order they happened, so its handlers run
// inline in the flush's job while the flush's locks are held. A handler that
// answers an ending with a new play schedules a task, which waits for the
// queue's lock and records into the next flush.
//
// There is no VoiceStartedEvent: an event tells a game something it did not
// do, and a start is the game's own Play. A stop is published at the flush
// that applied it, not when the backend's declick ramp ends, so it never
// means the speaker is quiet. Shutdown publishes nothing for the voices still
// live.
type VoiceEndedEvent struct {
	Voice  Voice
	Reason Reason
}

// Reason says why a voice ended. Every voice that ever existed is observable
// exactly once: it is in LiveVoices now, or its VoiceEndedEvent has fired.
type Reason uint8

const (
	// ReasonFinished is the clip reaching its end.
	ReasonFinished Reason = iota
	// ReasonStopped is Stop.
	ReasonStopped
	// ReasonStolen is the cap: a play that found every slot taken ends with
	// it in the flush that applies it, never having been live.
	ReasonStolen
	// ReasonFailed is a clip that could not be read or prepared. It arrives
	// one or more flushes after the play, never in the flush that applied it,
	// since preparing is asynchronous.
	ReasonFailed
	// ReasonReleased is the clip the voice was playing, or waiting on, being
	// released by Release or ReleaseAll. It arrives in the flush that applies
	// the release. It is not ReasonStopped: a release is not a stop, the
	// stopping is its side effect, and it is typically recorded far from the
	// play it cuts.
	ReasonReleased
)

// String renders a reason for a log and a failure message.
func (r Reason) String() string {
	switch r {
	case ReasonFinished:
		return "finished"
	case ReasonStopped:
		return "stopped"
	case ReasonStolen:
		return "stolen"
	case ReasonFailed:
		return "failed"
	case ReasonReleased:
		return "released"
	}
	return "unknown"
}
