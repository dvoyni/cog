package sfxtest

import "github.com/dvoyni/cog/kernel"

// logHandler is the harness's supervisor's kernel.LogHandler: it keeps each
// error logged, for Reports and for the harness to check, and drops warnings
// and debug lines.
type logHandler struct {
	harness *Harness
}

// LogErr keeps err for Reports, and so for the harness's next check.
func (l logHandler) LogErr(_ kernel.KernelID, err error) {
	l.harness.mutex.Lock()
	defer l.harness.mutex.Unlock()
	l.harness.reports = append(l.harness.reports, err)
}

// LogWarn drops the warning.
func (logHandler) LogWarn(kernel.KernelID, string, ...any) {}

// LogDebug drops the line.
func (logHandler) LogDebug(kernel.KernelID, string, ...any) {}
