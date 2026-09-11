package wgpu

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// quitCmdImpl stops the gogpu main loop, which unwinds Run and shuts the
// engine down.
func (p *Plugin) quitCmdImpl() (kernel.Lock, kernel.Execute[app.QuitRequest, app.QuitResponse]) {
	return nil, func(kernel.Kernel, app.QuitRequest) (app.QuitResponse, error) {
		p.gpu.Quit()
		return app.QuitResponse{}, nil
	}
}

// timeCmdImpl controls the tick source: it pauses update ticks, resumes them,
// steps a named number of them, and reports which is true.
//
// It declares no locks and takes none, which is what makes a step safe to wait
// on here: the handler blocks until the main thread has published its ticks,
// and it holds nothing that the tick, or the frame carrying it, could need.
func (p *Plugin) timeCmdImpl() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
	return nil, func(k kernel.Kernel, request app.TimeRequest) (app.TimeResponse, error) {
		return p.ticks.control(k.Context(), request)
	}
}
