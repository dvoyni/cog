package types

// MaxCaptureAmount caps one request at sixty stills, and MaxCaptureSpan caps
// the ticks they span. The interval is what buys a long window, never the
// amount. The
// span figure is app.TimeRequest's step cap, deliberately the same number so
// there is one figure to remember.
const (
	MaxCaptureAmount = 60
	MaxCaptureSpan   = 600
)
