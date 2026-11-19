package canvas

import "errors"

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

const (
	// ReportKindRecordedOutsideFrame is an entry recorded or a camera set
	// while no frame is recording: by a task on another beat, or by one
	// hung off render.FrameEvent that runs once canvas's drawing has begun.
	// It is reported once for each task.
	ReportKindRecordedOutsideFrame ReportKind = iota + 1
	// ReportKindCameraTargetRefused is a camera naming both a window and a
	// texture, or a texture render does not hold, which is skipped. It is
	// reported once for each target.
	ReportKindCameraTargetRefused
	// ReportKindPixelPerfectStretch is a pixel-perfect camera's FitStretch,
	// whose pixels would not be square, which is drawn as FitInscribe. It
	// is reported once.
	ReportKindPixelPerfectStretch
	// ReportKindImageOptionsIgnored is an image ImageFromFile was given more than
	// one set of options for, drawn with the first. It is reported once for
	// each path, at the image's first draw.
	ReportKindImageOptionsIgnored
)

// pixelPerfectStretchReport is the report of a pixel-perfect camera's
// FitStretch, made once, so that a camera drawing it each frame allocates
// nothing.
var pixelPerfectStretchReport error = Report{Kind: ReportKindPixelPerfectStretch, Err: errors.New("canvas: a pixel-perfect camera's FitStretch, whose pixels would not be square, is drawn as FitInscribe")}

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
