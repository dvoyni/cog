package internal

import (
	"github.com/dvoyni/cog/kernel"
)

// quitCmdImpl asks the MainLoop to stop the platform loop, which unwinds the Host's
// Run and shuts the engine down.
func (p *plugin) quitCmdImpl() (kernel.Lock, kernel.Execute[QuitRequest, QuitResponse]) {
	return nil, func(kernel.Kernel, QuitRequest) QuitResponse {
		p.mainLoop.Get().Quit()
		return QuitResponse{}
	}
}

// clipboardWriteCmdImpl hands the text to the MainLoop's clipboard.
func (p *plugin) clipboardWriteCmdImpl() (kernel.Lock, kernel.Execute[ClipboardWriteRequest, ClipboardWriteResponse]) {
	return nil, func(_ kernel.Kernel, request ClipboardWriteRequest) ClipboardWriteResponse {
		return ClipboardWriteResponse{Err: p.mainLoop.Get().ClipboardWrite(request.Text)}
	}
}

// timeCmdImpl controls the tick source: it pauses update ticks, resumes them,
// steps a named number of them, and reports which is true.
//
// It declares no locks and takes none, which is what makes a step safe to wait
// on here: the handler blocks until the MainLoop's main thread has published its
// ticks, and it holds nothing that the tick, or the frame carrying it, could
// need.
func (p *plugin) timeCmdImpl() (kernel.Lock, kernel.Execute[TimeRequest, TimeResponse]) {
	return nil, func(k kernel.Kernel, request TimeRequest) TimeResponse {
		return p.loop.ticks.control(request)
	}
}
