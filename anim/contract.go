// Package anim provides timelines of eased value tracks and one-tick cues.
//
// A Timeline is a clock plus a chain point. Add queues a track (a Sequence
// played over a duration with an easing) under a slot identified by the
// sequence type and an id; tracks under one slot append and play back to back.
// By default a track starts at the chain point, so successive adds narrate in
// order; Immediate starts it now, Rewind pulls the chain point back so tracks
// overlap, and Wait leaves a gap. Query and Value read the slot's current
// track. Cue queues a plain value that fires when the timeline reaches the
// chain point; Fired yields the cues that fired this tick.
//
// The plugin owns the Timelines resource and advances every timeline by the
// fixed step in the First phase of app.UpdateEvent, before ordinary handlers
// run. A handler binds kernel.Write[*anim.Timelines] (or Read, for queries
// alone) and takes timelines by key. A *Timeline is valid only for that
// handler pass: keep it in a field set on the way in and cleared on the way
// out, never across ticks. A nil *Timeline is the no-op timeline: writes are
// dropped and reads report nothing, which suits simulation that replays moves
// for their model mutations alone.
package anim

// Params describes how a track plays. The zero value is a zero-duration,
// linear, one-shot track that starts at the chain point.
type Params struct {
	// Duration is the track length in seconds. A track of zero or negative
	// duration completes on the first tick it is active.
	Duration float32
	// Easing shapes the track's progress before the sequence sees it; nil is
	// Linear.
	Easing Easing
	// Loop repeats the track forever, with Duration as the period. A looping
	// track is never dropped, does not move the chain point, and is ignored by
	// Timeline.Idle.
	Loop bool
	// Immediate starts the track now instead of at the chain point.
	Immediate bool
}

// Over returns Params for a one-shot linear track of the given duration that
// starts at the chain point.
func Over(duration float32) Params {
	return Params{Duration: duration}
}

// WithEasing returns a copy with the given easing.
func (p Params) WithEasing(easing Easing) Params {
	p.Easing = easing
	return p
}

// WithLoop returns a copy that repeats forever.
func (p Params) WithLoop() Params {
	p.Loop = true
	return p
}

// WithImmediate returns a copy that starts now instead of at the chain point.
func (p Params) WithImmediate() Params {
	p.Immediate = true
	return p
}

// State is the result of a Query: whether a track matched the slot and, if
// so, whether it is playing now or still pending.
type State uint8

const (
	// StateNotFound reports that no track is stored under the slot.
	StateNotFound State = iota
	// StatePending reports a track that is queued but has not started; its
	// progress is the easing of 0.
	StatePending
	// StateActive reports a track that is playing now.
	StateActive
)

// Found reports whether any track, active or pending, matched the query.
func (s State) Found() bool {
	return s != StateNotFound
}
