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
// anim is a Bundle. This package is its contract root and declares no plugin;
// the plugin and its tick handler are in animimpl, which only composition roots
// and tests import, and the code the two share is in internal.
//
// The plugin owns the Timelines resource and advances every timeline by the
// fixed step in the First phase of app.UpdateEvent, before ordinary handlers
// run; AdvanceOnUpdate identifies that subscription. A handler binds
// kernel.Write[*anim.Timelines] (or Read, for queries alone) and takes
// timelines by key. A *Timeline is valid only for that handler pass: keep it in
// a field set on the way in and cleared on the way out, never across ticks. A
// nil *Timeline is the no-op timeline: writes are dropped and reads report
// nothing, which suits simulation that replays moves for their model mutations
// alone.
package anim

import "github.com/dvoyni/cog/bundles/anim/internal"

// Params describes how a track plays. The zero value is a zero-duration,
// linear, one-shot track that starts at the chain point. WithEasing, WithLoop
// and WithImmediate return modified copies.
type Params = internal.Params

// Over returns Params for a one-shot linear track of the given duration that
// starts at the chain point.
func Over(duration float32) Params {
	return Params{Duration: duration}
}

// State is the result of a Query: whether a track matched the slot and, if
// so, whether it is playing now or still pending. State.Found reports whether
// any track, active or pending, matched.
type State = internal.State

const (
	// StateNotFound reports that no track is stored under the slot.
	StateNotFound = internal.StateNotFound
	// StatePending reports a track that is queued but has not started; its
	// progress is the easing of 0.
	StatePending = internal.StatePending
	// StateActive reports a track that is playing now.
	StateActive = internal.StateActive
)
