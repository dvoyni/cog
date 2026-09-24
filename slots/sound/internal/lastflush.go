package internal

// endingsHeld is how many endings the ring remembers. Thirty-two is a constant
// and not a knob: it is two ticks' worth of endings at the default Voice cap,
// which is more than a reader polling at any rate it can actually achieve will
// miss, and a number nobody tunes is a number nobody has to reason about.
const endingsHeld = 32

// ending is one entry of the ring: which Clip ended, why, and on which tick.
//
// It holds a ClipRef rather than a Voice because the question the ring answers
// is "did the alarm sound", not "what became of the handle I am holding". A
// reader that holds a handle has VoiceEndedEvent, which is ordered and
// lossless; a reader that cannot subscribe to anything has this.
type ending struct {
	clip   ClipRef
	reason Reason
	tick   int64
}

// lastFlush is what the flush leaves behind for the one reader that cannot
// subscribe to events: the tick it ran on, and a fixed ring of the last
// endingsHeld endings.
//
// It is the agent's alone. There is no kernel.Read-able counterpart and no
// game-facing type, because a game's test can subscribe to VoiceEndedEvent,
// which is unbounded and ordered, and an MCP client cannot subscribe to
// anything at all. Handing a reader that has the choice a capped, lossy ring
// over a lossless stream would be strictly worse for it; the ring exists
// because one reader has no choice. So the type is unexported in a package
// nothing outside slots/sound may import, which is that sentence spelled in
// the type system rather than promised in a comment.
//
// The ring is why *did the alarm sound* is answerable at all: a 400 ms one-shot
// is invisible between two tool calls, however fast the caller is, and the live
// view alone would report nothing and mean two different things by it.
//
// It is sized once, at registration, and allocates nothing afterwards: the
// entries are an array inside the resource and an ending overwrites one in
// place.
//
// Access it only while a handler holds its declared resource lock.
type lastFlush struct {
	// tick is the tick the last flush ran on, so a listing can name the moment
	// it describes. It is app.UpdateEvent.Tick and not a count of flushes: the
	// point of reporting it is that it is the same number canvas_draws,
	// ui_layout and gfx_frame report.
	tick int64
	// endings is the ring, next the index the following ending is written to,
	// and held how many of the entries have ever been written - which caps at
	// endingsHeld, so a ring that has not filled yet reports only what it
	// holds rather than a run of zero-valued endings.
	endings [endingsHeld]ending
	next    int
	held    int
}

// record stamps one ending with the tick it happened on and writes it over the
// oldest entry. It is called once per ending, beside the VoiceEndedEvent the
// same ending publishes, so the two never disagree about what ended.
func (l *lastFlush) record(e Ending, tick int64) {
	l.endings[l.next] = ending{clip: e.Clip, reason: e.Reason, tick: tick}
	l.next = (l.next + 1) % endingsHeld
	if l.held < endingsHeld {
		l.held++
	}
}

// each visits the endings it holds, oldest first, which is the order they
// happened in: a reader asking whether the alarm sounded before the door opened
// reads them the way the game played them.
func (l *lastFlush) each(visit func(ending)) {
	start := (l.next - l.held + endingsHeld) % endingsHeld
	for i := 0; i < l.held; i++ {
		visit(l.endings[(start+i)%endingsHeld])
	}
}
