//go:build !kernel_schedtrace

package kernel

// lockTrace is a job's timeline through the scheduler. It is empty here, and
// every hook below is a no-op the compiler removes, so the scheduler in this
// build is byte for byte the untraced one.
//
// Build with -tags kernel_schedtrace for the other file, which records it for
// the lock load test.
type lockTrace struct{}

func traceSubmitted(*jobSlot)      {}
func traceGranted(*jobSlot, jobID) {}
func traceFinishing(*jobSlot)      {}
