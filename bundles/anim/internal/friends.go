package internal

// The friend functions: what the anim root and animimpl do to a public type's
// unexported state. Only the root and animimpl can import this package, so
// these are not public API.

// TimelinesAdvance calls Timelines.advance for the anim root and animimpl.
func TimelinesAdvance(v *Timelines, dt float32) { v.advance(dt) }
