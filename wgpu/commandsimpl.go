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
