//go:build !windows

package gogpu

// isDeviceRemoved answers false: DX12, whose device is removed, runs on
// Windows alone.
func isDeviceRemoved(error) bool { return false }
