package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// quitCmdImpl asks the MainLoop to stop the platform loop, which unwinds the Host's
// Run and shuts the engine down.
func (p *plugin) quitCmdImpl() (kernel.Lock, kernel.Execute[app.QuitRequest, app.QuitResponse]) {
	return nil, func(kernel.Kernel, app.QuitRequest) (app.QuitResponse, error) {
		p.mainLoop.Get().Quit()
		return app.QuitResponse{}, nil
	}
}

// timeCmdImpl controls the tick source: it pauses update ticks, resumes them,
// steps a named number of them, and reports which is true.
//
// It declares no locks and takes none, which is what makes a step safe to wait
// on here: the handler blocks until the MainLoop's main thread has published its
// ticks, and it holds nothing that the tick, or the frame carrying it, could
// need.
func (p *plugin) timeCmdImpl() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
	return nil, func(k kernel.Kernel, request app.TimeRequest) (app.TimeResponse, error) {
		return p.loop.ticks.control(k.Context(), request)
	}
}
