package internal

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
