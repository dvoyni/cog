// Package anim declares timelines of eased value tracks and one-tick cues.
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
// anim is a Bundle. Its plugin, built by animplugin.New, requires no Adapter
// and contributes none.
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
