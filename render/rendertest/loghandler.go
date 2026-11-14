package rendertest

import "github.com/dvoyni/cog/kernel"

// logHandler is the harness's supervisor's kernel.LogHandler: it keeps each
// error logged for the harness to check, and drops warnings and debug lines.
type logHandler struct {
	harness *Harness
}

// LogErr keeps err for the harness's next check.
func (l logHandler) LogErr(_ kernel.KernelID, err error) {
	l.harness.mutex.Lock()
	defer l.harness.mutex.Unlock()
	l.harness.logged = append(l.harness.logged, err)
}

// LogWarn drops the warning.
func (logHandler) LogWarn(kernel.KernelID, string, ...any) {}

// LogDebug drops the line.
func (logHandler) LogDebug(kernel.KernelID, string, ...any) {}
