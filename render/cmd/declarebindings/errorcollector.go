package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/dvoyni/cog/kernel"
)

// errorCollector is the supervisor's log handler: it keeps every error
// logged, and writes warnings to the standard error.
type errorCollector struct {
	mutex  sync.Mutex
	errors []error
}

// LogErr keeps err.
func (c *errorCollector) LogErr(_ kernel.KernelID, err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.errors = append(c.errors, err)
}

// LogWarn writes message, with args, to the standard error.
func (*errorCollector) LogWarn(_ kernel.KernelID, message string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"declarebindings: warning:", message}, args...)...)
}

// LogDebug does nothing.
func (*errorCollector) LogDebug(kernel.KernelID, string, ...any) {}
