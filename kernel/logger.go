package kernel

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Logger is where every kernel's output goes. A Supervisor has one, and each
// Kernel forwards to it under its own ID, so source names the kernel the
// output came from.
type Logger interface {
	// LogErr is told of err, and is the only place an error goes: the kernel
	// neither collects nor judges it, and carries on. A failure that should end
	// kernel source is ended here, by the Logger calling Supervisor.Destroy or
	// Supervisor.Quit; one that should end the process may panic.
	//
	// A job or handler that panics is recovered and its panic passed here as
	// err, with the stack it panicked on, on the goroutine that ran it.
	LogErr(source KernelID, err error)

	// LogWarn logs msg with args as alternating keys and values, as log/slog
	// does.
	LogWarn(source KernelID, msg string, args ...any)

	// LogDebug logs msg with args as alternating keys and values, as log/slog
	// does.
	LogDebug(source KernelID, msg string, args ...any)

	// LogErrOnce, LogWarnOnce and LogDebugOnce log as their plain forms do the
	// first time they are called under key for source, and drop every call
	// after, until ForgetLogOnce clears key. They are for a condition true every
	// frame, worth saying once and noise at the frame rate. key names the
	// condition, such as "sprite:missing:" plus a path; keys of different
	// kernels never collide. A constant key costs nothing to pass, while one
	// built at run time allocates.
	LogErrOnce(source KernelID, key string, err error)
	LogWarnOnce(source KernelID, key string, msg string, args ...any)
	LogDebugOnce(source KernelID, key string, msg string, args ...any)

	// ForgetLogOnce clears key for source, so the next Once call under it logs
	// again. A condition that was repaired, and could fail again, must be
	// forgotten or it stays silent for the kernel's life.
	ForgetLogOnce(source KernelID, key string)
}

var _ Logger = (*StdLogger)(nil)

// StdLogger is the Logger a Supervisor created with a nil one uses. LogErr
// panics, LogWarn writes to stderr and LogDebug to stdout, one line each,
// prefixed with the source kernel's ID. Its zero value is ready to use.
type StdLogger struct {
	mu     sync.Mutex
	logged map[onceKey]struct{}
}

// onceKey is a Once key scoped to the kernel that logged it.
type onceKey struct {
	source KernelID
	key    string
}

// LogErr panics with err, wrapped with source.
func (l *StdLogger) LogErr(source KernelID, err error) {
	panic(fmt.Errorf("kernel %d: %w", source, err))
}

// LogWarn writes msg and args to stderr.
func (l *StdLogger) LogWarn(source KernelID, msg string, args ...any) {
	writeLine(os.Stderr, source, msg, args)
}

// LogDebug writes msg and args to stdout.
func (l *StdLogger) LogDebug(source KernelID, msg string, args ...any) {
	writeLine(os.Stdout, source, msg, args)
}

// LogErrOnce panics with err the first time key is seen for source.
func (l *StdLogger) LogErrOnce(source KernelID, key string, err error) {
	if l.first(source, key) {
		l.LogErr(source, err)
	}
}

// LogWarnOnce writes to stderr the first time key is seen for source.
func (l *StdLogger) LogWarnOnce(source KernelID, key string, msg string, args ...any) {
	if l.first(source, key) {
		l.LogWarn(source, msg, args...)
	}
}

// LogDebugOnce writes to stdout the first time key is seen for source.
func (l *StdLogger) LogDebugOnce(source KernelID, key string, msg string, args ...any) {
	if l.first(source, key) {
		l.LogDebug(source, msg, args...)
	}
}

// ForgetLogOnce clears key for source.
func (l *StdLogger) ForgetLogOnce(source KernelID, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.logged, onceKey{source, key})
}

// first records key for source and reports whether it was new.
func (l *StdLogger) first(source KernelID, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := onceKey{source, key}
	if _, seen := l.logged[k]; seen {
		return false
	}
	if l.logged == nil {
		l.logged = map[onceKey]struct{}{}
	}
	l.logged[k] = struct{}{}
	return true
}

// writeLine writes source and msg, then args as key=value pairs. A trailing
// key with no value is written as a bare value.
func writeLine(w io.Writer, source KernelID, msg string, args []any) {
	var line strings.Builder
	fmt.Fprintf(&line, "[kernel %d] %s", source, msg)
	for i := 0; i < len(args); i += 2 {
		if i+1 == len(args) {
			fmt.Fprintf(&line, " %v", args[i])
			break
		}
		fmt.Fprintf(&line, " %v=%v", args[i], args[i+1])
	}
	line.WriteByte('\n')
	io.WriteString(w, line.String())
}
