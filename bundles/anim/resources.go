package anim

// Timelines is the plugin's resource: every timeline in the engine, keyed by
// an arbitrary comparable value and all advanced by UpdateEventHandler each
// tick. Bind kernel.Write[*anim.Timelines] to add tracks and cues, or
// kernel.Read[*anim.Timelines] to query alone (use Lookup, which does not
// create). A *Timeline taken from it is valid only for the handler pass that
// took it.
type Timelines = timelines
