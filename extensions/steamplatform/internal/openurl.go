//go:build windows || linux || darwin

package internal

import (
	"os/exec"
	"runtime"
)

// openURL asks the system to open url with whatever it has registered for the
// scheme: the Steam client for steam://, the default browser for https://. The
// command is started and not waited for, so an error is only one that could
// not start it.
func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
