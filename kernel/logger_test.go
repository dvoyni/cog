package kernel

import (
	"errors"
	"slices"
	"sync"
	"testing"
)

// lineLogHandler keeps every call as source and message.
type lineLogHandler struct {
	mutex sync.Mutex
	lines []string
}

func (h *lineLogHandler) record(source KernelID, msg string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.lines = append(h.lines, string(rune('0'+source))+":"+msg)
}

func (h *lineLogHandler) LogErr(source KernelID, err error)              { h.record(source, err.Error()) }
func (h *lineLogHandler) LogWarn(source KernelID, msg string, _ ...any)  { h.record(source, msg) }
func (h *lineLogHandler) LogDebug(source KernelID, msg string, _ ...any) { h.record(source, msg) }

func TestLoggerForwardsUnderItsKernel(t *testing.T) {
	handler := &lineLogHandler{}
	logger := &Logger{kernel: 3, handler: handler}
	logger.LogErr(errors.New("err"))
	logger.LogWarn("warn")
	logger.LogDebug("debug")
	if want := []string{"3:err", "3:warn", "3:debug"}; !slices.Equal(handler.lines, want) {
		t.Fatalf("handler got %v, want %v", handler.lines, want)
	}
}

func TestLoggerOnceLogsUntilForgotten(t *testing.T) {
	handler := &lineLogHandler{}
	logger := &Logger{kernel: 1, handler: handler}
	for range 3 {
		logger.LogWarnOnce("missing", "warn")
		logger.LogDebugOnce("slow", "debug")
		logger.LogErrOnce("broken", errors.New("err"))
	}
	logger.ForgetLogOnce("missing")
	logger.LogWarnOnce("missing", "warn again")
	want := []string{"1:warn", "1:debug", "1:err", "1:warn again"}
	if !slices.Equal(handler.lines, want) {
		t.Fatalf("handler got %v, want %v", handler.lines, want)
	}
}

func TestLoggerOnceKeysAreKernelScoped(t *testing.T) {
	handler := &lineLogHandler{}
	first := &Logger{kernel: 1, handler: handler}
	second := &Logger{kernel: 2, handler: handler}
	first.LogWarnOnce("key", "warn")
	second.LogWarnOnce("key", "warn")
	if want := []string{"1:warn", "2:warn"}; !slices.Equal(handler.lines, want) {
		t.Fatalf("handler got %v, want %v", handler.lines, want)
	}
}

func TestLoggerOnceConcurrentLogsOnce(t *testing.T) {
	handler := &lineLogHandler{}
	logger := &Logger{kernel: 1, handler: handler}
	var wait sync.WaitGroup
	for range 16 {
		wait.Go(func() { logger.LogWarnOnce("key", "warn") })
	}
	wait.Wait()
	if len(handler.lines) != 1 {
		t.Fatalf("handler got %v, want one line", handler.lines)
	}
}

func TestStdLoggerErrPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("StdLogger.LogErr did not panic")
		}
	}()
	(&StdLogger{}).LogErr(1, errors.New("err"))
}
