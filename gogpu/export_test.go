package gogpu

import "github.com/dvoyni/cog/platform"

// RequestClose runs the close callback of window's gogpu window, as the
// operating system does at the user's close, and answers what it answers:
// true closes the window.
func RequestClose(p *Plugin, window platform.Window) bool { return p.requestClose(window) }
