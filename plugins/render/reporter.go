package render

import (
	"errors"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
)

// reporter is what a resource of render's that reports holds: the kernel's
// logger, where reports and errors go, and the inspector, which every
// resource that reports shares, where the reports of a frame inspected go
// too and render's calls write that frame down.
type reporter struct {
	logger    *kernel.Logger
	inspector *inspector
}

// report logs a Report of kind, saying err, and adds it to the inspection of
// the frame recording, if any.
func (r reporter) report(kind ReportKind, err error) {
	report := Report{Kind: kind, Err: err}
	r.logger.LogErr(report)
	r.inspector.add(func(inspection *Inspection) { inspection.Reports = append(inspection.Reports, report) })
}

// logUnlessLost logs err, unless the device is lost, which gfx has logged.
func (r reporter) logUnlessLost(err error) {
	if !errors.Is(err, gfx.ErrDeviceLost) {
		r.logger.LogErr(err)
	}
}
