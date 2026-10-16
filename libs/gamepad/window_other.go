//go:build !windows

package gamepad

// nativeWindowRequired is true where the backend cannot poll without a window:
// the Windows backend hears hot-plug by subclassing the window procedure.
const nativeWindowRequired = false
