package sfxtest

import (
	"slices"

	"github.com/dvoyni/cog/kernel"
)

// logHandler is the harness's supervisor's kernel.LogHandler: it keeps each
// error logged, for Reports and for the harness to check, and each warning,
// for Warnings, and drops debug lines.
type logHandler struct {
	harness *Harness
}

// LogErr keeps err for Reports, and so for the harness's next check.
func (l logHandler) LogErr(_ kernel.KernelID, err error) {
	l.harness.mutex.Lock()
	defer l.harness.mutex.Unlock()
	l.harness.reports = append(l.harness.reports, err)
}

// LogWarn keeps the warning, for Warnings.
func (l logHandler) LogWarn(_ kernel.KernelID, message string, args ...any) {
	l.harness.mutex.Lock()
	defer l.harness.mutex.Unlock()
	l.harness.warnings = append(l.harness.warnings, Warning{Message: message, Args: slices.Clone(args)})
}

// LogDebug drops the line.
func (logHandler) LogDebug(kernel.KernelID, string, ...any) {}
