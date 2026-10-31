package internal

// Reason says why a Voice ended. Every ending has one, and every Voice that
// ever existed is observable exactly once: it is in the live view now, or its
// ending has fired.
//
// The delays differ, and a game reading only the reason would assume they do
// not. ReasonStolen from a full table arrives in the same flush as the play;
// ReasonFailed arrives one or more ticks later, never in the same tick, because
// preparing is asynchronous. A game cannot write "play it, and if it fails this
// frame, do X".
type Reason uint8

const (
	// ReasonFinished is the Clip reaching its end while not looping. A looping
	// Voice never ends by itself.
	ReasonFinished Reason = iota
	// ReasonStopped is Stop, or a StopBus covering it. StopBus does not get its
	// own member: it is Stop over a set, splitting it later is additive, and a
	// reader switching on ReasonStopped keeps working.
	ReasonStopped
	// ReasonStolen is the cap, whether the Voice played for a second or never
	// sounded.
	ReasonStolen
	// ReasonFailed is a Clip that could not be read or prepared.
	ReasonFailed
	// ReasonReleased is the Clip the Voice was playing being released. It is
	// not folded into ReasonStopped because Release is not a stop at all -
	// stopping is its side effect - and folding it would hide the one
	// release-related bug behind the one cause that is always innocent.
	ReasonReleased
)

// String renders a Reason for a log and a failure message.
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

// Ending is one Voice's ending, collected during a flush and published as a
// VoiceEndedEvent by the handler that ran it. It is a value of its own rather
// than the event because it carries the Clip, which the event does not.
type Ending struct {
	Voice  Voice
	Reason Reason
	// Clip is what the Voice was playing. It rides here because this is the
	// only moment it can: the slot is cleared at the end of the flush, and the
	// endings are read after that, so an ending that did not carry its Clip
	// could never be told which sound it was about. VoiceEndedEvent does not
	// carry it - a game holds the handle it played with and already knows -
	// and the reader that does not is the endings ring, which is why it is
	// here rather than on the event.
	Clip ClipRef
}
