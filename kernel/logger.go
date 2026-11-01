package kernel

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Logger is one kernel's way to its Supervisor's LogHandler: every call goes
// to the handler under the kernel's ID. It also keeps the kernel's Once keys,
// so the handler never sees a repeat and keys of different kernels never
// collide. Kernel.Logger returns it, and it is safe from any goroutine.
type Logger struct {
	kernel  KernelID
	handler LogHandler

	// mutex guards logged, the Once keys logged and not yet forgotten.
	mutex  sync.Mutex
	logged map[string]struct{}
}

// LogErr tells the handler of err. It is the only place an error goes: the
// kernel neither collects nor judges it, and carries on.
func (l *Logger) LogErr(err error) {
	l.handler.LogErr(l.kernel, err)
}

// LogErrOnce is LogErr the first time it is called under key, and does nothing
// after, until ForgetLogOnce clears key.
func (l *Logger) LogErrOnce(key string, err error) {
	if l.first(key) {
		l.LogErr(err)
	}
}

// LogWarn logs msg with args as alternating keys and values, as log/slog does.
func (l *Logger) LogWarn(msg string, args ...any) {
	l.handler.LogWarn(l.kernel, msg, args...)
}

// LogWarnOnce is LogWarn the first time it is called under key, and does
// nothing after, until ForgetLogOnce clears key.
func (l *Logger) LogWarnOnce(key string, msg string, args ...any) {
	if l.first(key) {
		l.LogWarn(msg, args...)
	}
}

// LogDebug logs msg with args as alternating keys and values, as log/slog
// does.
func (l *Logger) LogDebug(msg string, args ...any) {
	l.handler.LogDebug(l.kernel, msg, args...)
}

// LogDebugOnce is LogDebug the first time it is called under key, and does
// nothing after, until ForgetLogOnce clears key.
func (l *Logger) LogDebugOnce(key string, msg string, args ...any) {
	if l.first(key) {
		l.LogDebug(msg, args...)
	}
}

// ForgetLogOnce clears key, so the next Once call under it logs again. A
// condition that was repaired, and could fail again, must be forgotten or it
// stays silent for the kernel's life.
func (l *Logger) ForgetLogOnce(key string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	delete(l.logged, key)
}

// first records key and reports whether it was new.
func (l *Logger) first(key string) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, seen := l.logged[key]; seen {
		return false
	}
	if l.logged == nil {
		l.logged = map[string]struct{}{}
	}
	l.logged[key] = struct{}{}
	return true
}

// LogHandler is where every kernel's output goes. A Supervisor has one, and each
// Kernel forwards to it under its own ID, so source names the kernel the
// output came from.
type LogHandler interface {
	// LogErr is told of err, and is the only place an error goes: the kernel
	// neither collects nor judges it, and carries on. A failure that should end
	// kernel source is ended here, by the LogHandler calling Supervisor.Destroy or
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
}

var _ LogHandler = (*StdLogger)(nil)

// StdLogger is the LogHandler a Supervisor created with a nil one uses. LogErr
// panics, LogWarn writes to stderr and LogDebug to stdout, one line each,
// prefixed with the source kernel's ID. Its zero value is ready to use.
type StdLogger struct {
	// mutex keeps lines from different kernels whole.
	mutex sync.Mutex
}

// LogErr panics with err, wrapped with source.
func (l *StdLogger) LogErr(source KernelID, err error) {
	panic(fmt.Errorf("kernel %d: %w", source, err))
}

// LogWarn writes msg and args to stderr.
func (l *StdLogger) LogWarn(source KernelID, msg string, args ...any) {
	l.writeLine(os.Stderr, source, msg, args)
}

// LogDebug writes msg and args to stdout.
func (l *StdLogger) LogDebug(source KernelID, msg string, args ...any) {
	l.writeLine(os.Stdout, source, msg, args)
}

// writeLine writes source and msg, then args as key=value pairs. A trailing
// key with no value is written as a bare value.
func (l *StdLogger) writeLine(w io.Writer, source KernelID, msg string, args []any) {
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
	l.mutex.Lock()
	defer l.mutex.Unlock()
	io.WriteString(w, line.String())
}
