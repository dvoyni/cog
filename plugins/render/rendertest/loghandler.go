package rendertest

import (
	"errors"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// logHandler is the harness's supervisor's kernel.LogHandler: it keeps each
// error logged for the harness to check, and each render.Report for Reports,
// and drops warnings and debug lines.
type logHandler struct {
	harness *Harness
}

// LogErr keeps err for the harness's next check, and for Reports when it is
// a render.Report.
func (l logHandler) LogErr(_ kernel.KernelID, err error) {
	l.harness.mutex.Lock()
	defer l.harness.mutex.Unlock()
	l.harness.logged = append(l.harness.logged, err)
	if report, isReport := errors.AsType[render.Report](err); isReport {
		l.harness.reports = append(l.harness.reports, report)
	}
}

// LogWarn drops the warning.
func (logHandler) LogWarn(kernel.KernelID, string, ...any) {}

// LogDebug drops the line.
func (logHandler) LogDebug(kernel.KernelID, string, ...any) {}
