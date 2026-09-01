//go:build !js

package wgpu

import "runtime"

// gogpu must own OS thread 0 for the window/UI (macOS AppKit). Package init runs
// on the main goroutine before main, so locking it here pins the main goroutine
// before Kernel.Run hands it to the system plugin. This is desktop-only: wasm is
// single-threaded and LockOSThread would only interfere with the browser's async
// scheduling.
func init() {
	runtime.LockOSThread()
}
