//go:build !windows

package internal

// findGamepadWindow returns 0: only the Windows gamepad layer needs a window.
func findGamepadWindow() uintptr { return 0 }
