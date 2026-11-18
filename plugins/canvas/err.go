package canvas

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

const (
	// ReportKindEntryOutsideFrame is an entry recorded while no frame is
	// recording: by a task on another beat, or by one hung off
	// render.FrameEvent that runs once canvas's drawing has begun. It is
	// reported once for each task.
	ReportKindEntryOutsideFrame ReportKind = iota + 1
)

// Report is a mistake canvas found at run time and carried past, logged
// through the kernel's LogErrOnce, so each is logged once for its subject,
// and typed, so a test can tell which it is.
type Report struct {
	Kind ReportKind
	Err  error
}

// Error answers Err's message.
func (r Report) Error() string { return r.Err.Error() }

// Unwrap answers Err.
func (r Report) Unwrap() error { return r.Err }
