package gogpu

import (
	"github.com/dvoyni/cog/platform"
	"github.com/gogpu/gpucontext"
)

// RequestClose runs the close callback of window's gogpu window, as the
// operating system does at the user's close, and answers what it answers:
// true closes the window.
func RequestClose(p *Plugin, window platform.Window) bool { return p.requestClose(window) }

// ReportPointer reports event in window, as the plugin's pointer callback on
// window's gogpu window does when the operating system calls it.
func ReportPointer(p *Plugin, window platform.Window, event gpucontext.PointerEvent) {
	p.reportPointer(window, event)
}
