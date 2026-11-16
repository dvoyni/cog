package rendertest

import (
	"os"
	"runtime"
	"testing"
)

// mainThreadWork takes the work RunMain runs on the process's main thread:
// nil unless the test binary's TestMain calls RunMain, which sets it before
// any test runs.
var mainThreadWork chan func()

// init keeps the main goroutine on the process's main thread, where package
// inits run, so RunMain, called from TestMain on it, runs there.
func init() { runtime.LockOSThread() }

// RunMain runs m's tests and exits with their code, as a TestMain does,
// keeping the process's main thread for the harnesses on the software
// adapter. A test package whose tests call UseSoftwareAdapter calls it from
// its TestMain:
//
//	func TestMain(m *testing.M) { rendertest.RunMain(m) }
//
// The tests run on a goroutine of their own, while the main thread runs
// what each such harness hands it: its Spawn, each tick of the kernel's
// main thread, and its quit. gogpu's HostLoop then runs on the process's
// main thread, as macOS requires. Without RunMain, a harness on the
// software adapter locks its test's goroutine to an OS thread instead,
// which serves on Windows and Linux, and skips the test on macOS.
//
// It is called once, from TestMain, and panics when called again.
func RunMain(m *testing.M) {
	if mainThreadWork != nil {
		panic("rendertest: RunMain called twice")
	}
	work := make(chan func())
	mainThreadWork = work
	exitCode := make(chan int)
	go func() { exitCode <- m.Run() }()
	for {
		select {
		case run := <-work:
			run()
		case code := <-exitCode:
			os.Exit(code)
		}
	}
}
