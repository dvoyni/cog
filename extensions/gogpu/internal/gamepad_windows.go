//go:build windows

package internal

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowSearch is what findGamepadWindow's callback looks for and finds.
type windowSearch struct {
	thread uint32
	window windows.HWND
}

// enumWindowCallback is made once: Windows keeps every callback a process
// makes for its whole life, and only a couple of thousand fit.
var enumWindowCallback = windows.NewCallback(func(hwnd windows.HWND, search *windowSearch) uintptr {
	if thread, _ := windows.GetWindowThreadProcessId(hwnd, nil); thread == search.thread &&
		windows.IsWindowVisible(hwnd) {
		search.window = hwnd
		return 0
	}
	return 1
})

// findGamepadWindow finds the game's window, which the gamepad layer listens to
// for hot-plug: the visible top-level window owned by the calling thread. It
// runs on the main thread, where gogpu made its window, so this finds it
// without gogpu handing out its HWND. It returns 0 while there is no window
// yet, and the poll waits for one.
func findGamepadWindow() uintptr {
	search := windowSearch{thread: windows.GetCurrentThreadId()}
	// EnumWindows reports the callback stopping it early as a failure.
	_ = windows.EnumWindows(enumWindowCallback, unsafe.Pointer(&search))
	return uintptr(search.window)
}
