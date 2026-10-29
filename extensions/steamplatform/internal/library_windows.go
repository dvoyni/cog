//go:build windows

package internal

import "syscall"

// steamLibrary is the 64-bit Windows Steam library.
const steamLibrary = "steam_api64.dll"

func openLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, err
	}
	return uintptr(handle), nil
}
