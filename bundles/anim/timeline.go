package anim

import "github.com/dvoyni/cog/bundles/anim/internal"

// Timeline is one chain of tracks and cues with its own clock; see the package
// documentation for the chain-point model. It is not a resource of its own:
// timelines are handed out by the Timelines resource and are valid only for the
// handler pass that took them. The zero value is an empty timeline at time zero,
// and a nil *Timeline is the no-op timeline.
//
// Add, Query and Value queue and read tracks; Cue, Fired and FiredCues queue
// and read cues; Rewind and Wait move the chain point; Time, Idle and Reset
// read and clear the clock.
type Timeline = internal.Timeline
