package types

// The friend functions: what anim's internal/ does to a public type's
// unexported state. Only packages under bundles/anim can import this package,
// so these are not public API.

// TimelinesAdvance calls Timelines.advance for anim's internal/.
func TimelinesAdvance(v *Timelines, dt float32) { v.advance(dt) }
