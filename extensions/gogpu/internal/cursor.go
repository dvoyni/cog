package internal

import "github.com/gogpu/gpucontext"

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
