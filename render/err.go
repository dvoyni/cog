package render

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

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
