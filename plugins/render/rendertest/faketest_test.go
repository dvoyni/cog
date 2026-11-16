package rendertest_test

import (
	"fmt"
	"sync"
	"testing"
)

// fakeTest is the test the harness runs under, keeping each failure the
// harness reports in place of failing the real test.
type fakeTest struct {
	testing.TB
	mutex    sync.Mutex
	failures []string
}

func (f *fakeTest) Errorf(format string, args ...any) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failures = append(f.failures, fmt.Sprintf(format, args...))
}

// takeFailures answers the failures reported since it was last called.
func (f *fakeTest) takeFailures() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	failures := f.failures
	f.failures = nil
	return failures
}
