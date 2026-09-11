package ui

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// registerCommands declares ui's own commands. There is one: the snapshot arm,
// which is ordinary ui API that the agent-facing capability happens to be the
// first caller of.
func (p *Plugin) registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[ArmLayoutCmd](p.armLayoutCmdImpl)
}

// armLayoutCmdImpl installs the tick's one snapshot request and hands back the
// channel the result arrives on, plus the viewport the caller cannot read for
// itself. The Viewport read is the only lock it needs: the snapshot slot is
// plugin-owned and carries its own.
func (p *Plugin) armLayoutCmdImpl() (kernel.Lock, kernel.Execute[ArmLayoutRequest, ArmLayoutResponse]) {
	var viewport kernel.Read[*app.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*app.Viewport]()
		}, func(_ kernel.Kernel, request ArmLayoutRequest) (ArmLayoutResponse, error) {
			live, err := p.snapshots.arm(request)
			if err != nil {
				return ArmLayoutResponse{}, err
			}
			return ArmLayoutResponse{Done: live.done, Viewport: *viewport.Get()}, nil
		}
}
