package render

import "time"

// PassTiming is how long the GPU took to run one pass or compute pass of a
// frame timed while Frame.SetPassTimings(true) held: the order of the
// command list it was recorded in, math.MinInt32 for render's own, its
// label, and the time from its beginning to its end.
type PassTiming struct {
	Order    Order
	Label    string
	Duration time.Duration
}
