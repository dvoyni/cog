package canvas

import "github.com/dvoyni/cog/kernel"

// reporter is where canvas's reports go: each is logged through the
// kernel's logger, once for its key until the key is forgotten, and one
// logged while an inspection is pending is written down by the inspector
// for the frame it is drawn into.
type reporter struct {
	logger    *kernel.Logger
	inspector *inspector
}

// reportOnce logs report, a Report, the first time it is called under key,
// until forget clears key, and writes it down for the inspection pending. It
// takes the report as an error, so a report kept as one, as a command list
// keeps its own, is not boxed again at each call.
func (r *reporter) reportOnce(key string, report error) {
	canvasReport, isReport := report.(Report)
	if !isReport {
		panic("canvas: reporting an error that is no canvas.Report")
	}
	if r.logger.LogErrOnce(key, report) {
		r.inspector.addReport(canvasReport)
	}
}

// forget clears key, so the next report under it is logged again.
func (r *reporter) forget(key string) {
	r.logger.ForgetLogOnce(key)
}
