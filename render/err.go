package render

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

const (
	// ReportKindOrderTaken is a command list begun at an order another list
	// of the frame has.
	ReportKindOrderTaken ReportKind = iota + 1
	// ReportKindOrderReserved is a command list begun at render's own order,
	// math.MinInt32.
	ReportKindOrderReserved
	// ReportKindListOutsideFrame is a command list begun while no frame is
	// recording.
	ReportKindListOutsideFrame
	// ReportKindPassTargetsRefused is a pass refused for its colour targets:
	// none, more than the device allows, one naming no window, or targets of
	// different sizes.
	ReportKindPassTargetsRefused
	// ReportKindLoadFailed is a loaded asset whose file could not be read or
	// decoded, such as a texture that draws magenta in its place.
	ReportKindLoadFailed
	// ReportKindReleaseRefused is a release of an asset no acquire holds: a
	// temporary, one released as often as it was acquired, or the zero
	// handle.
	ReportKindReleaseRefused
)

// Report is a mistake render finds at run time, such as a refused shader, a
// duplicate order or a draw skipped for a missing binding: logged once through
// the kernel's LogErr, and carried past. Its kind names the mistake, and its
// Err says what went wrong and where.
type Report struct {
	Kind ReportKind
	Err  error
}

// Error answers Err's message.
func (r Report) Error() string { return r.Err.Error() }

// Unwrap answers Err.
func (r Report) Unwrap() error { return r.Err }
