package sfx

import "time"

// clipBounds is how long a voice's clip is, and the span a looping voice on it
// repeats between: the clip's loop region, or the whole clip when it declares
// none or one the clip does not contain. Every playhead sfx keeps, and every
// offset it hands the backend, has been through fit.
type clipBounds struct {
	duration  time.Duration
	loopStart time.Duration
	loopEnd   time.Duration
}

// fit answers where a voice asked to be at playhead is, and whether it is a
// one-shot that has run out. A negative playhead is the head. A one-shot at or
// past the clip's end has run out, and stands at the end. A looping voice at or
// past its loop end is carried back inside its loop region by whole spans, so
// one placed in the clip's tail, or past the clip altogether, stands where a
// voice that had been looping all along would. A playhead short of the loop end
// is the intro or the loop itself, and neither moves: a looping voice runs into
// its loop, never lifted up to it.
func (b clipBounds) fit(playhead time.Duration, looping bool) (time.Duration, bool) {
	playhead = max(playhead, 0)
	if !looping {
		if playhead >= b.duration {
			return b.duration, true
		}
		return playhead, false
	}
	if playhead < b.loopEnd {
		return playhead, false
	}
	span := b.loopEnd - b.loopStart
	if span <= 0 {
		return b.loopStart, false
	}
	return b.loopStart + (playhead-b.loopStart)%span, false
}
