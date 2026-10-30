package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/gogpu/gpucontext"
)

// applyCursor shows or hides the cursor as app.CursorCmd last asked, when that
// differs from what the window was last told. Called at the start of onUpdate,
// on the main thread: a window's cursor belongs to the thread that owns it.
func (p *plugin) applyCursor() {
	hidden := p.cursorHidden.Load()
	if hidden == p.cursorApplied {
		return
	}
	p.cursorApplied = hidden
	if hidden {
		p.gpu.SetCursor(gpucontext.CursorNone)
		return
	}
	p.gpu.SetCursor(gpucontext.CursorDefault)
}

// applyFullscreen puts the window into fullscreen or out of it as
// app.FullscreenCmd last asked, when it has asked since the last call and the
// window is not that way already. Called at the start of onUpdate, on the main
// thread, like applyCursor.
func (p *plugin) applyFullscreen() {
	if !p.fullscreenPending.Swap(false) {
		return
	}
	if fullscreen := p.fullscreen.Load(); fullscreen != p.gpu.IsFullscreen() {
		p.gpu.SetFullscreen(fullscreen)
	}
}

// reportMonitorSize hands the Loop the device-pixel size of the window's
// monitor when it differs from what was last reported: the first frame the
// window exists, and after the window moves to another monitor. Called in
// onUpdate, on the main thread that owns the window, like applyCursor. A
// platform that cannot tell reports nothing.
func (p *plugin) reportMonitorSize(k kernel.Executioner) {
	if p.gpu == nil {
		return
	}
	width, height, ok := p.gpu.MonitorSize()
	if !ok || (width == p.monitorWidth && height == p.monitorHeight) {
		return
	}
	p.monitorWidth, p.monitorHeight = width, height
	p.loop.MonitorSize(k, width, height)
}
