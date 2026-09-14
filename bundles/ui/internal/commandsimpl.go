package internal

import (
	"github.com/dvoyni/cog/bundles/ui"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
)

// registerCommands declares ui's own commands. There is one: the snapshot arm,
// which is ordinary ui API that the agent-facing capability happens to be the
// first caller of.
func (p *plugin) registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[ui.ArmLayoutCmd](p.armLayoutCmdImpl)
}

// armLayoutCmdImpl installs the tick's one snapshot request and hands back the
// channel the result arrives on, plus the viewport the caller cannot read for
// itself. The Viewport read is the only lock it needs: the snapshot slot is
// plugin-owned and carries its own.
func (p *plugin) armLayoutCmdImpl() (kernel.Lock, kernel.Execute[ui.ArmLayoutRequest, ui.ArmLayoutResponse]) {
	var viewport kernel.Read[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request ui.ArmLayoutRequest) (ui.ArmLayoutResponse, error) {
			live, err := p.snapshots.arm(request)
			if err != nil {
				return ui.ArmLayoutResponse{}, err
			}
			return ui.ArmLayoutResponse{Done: live.done, Viewport: *viewport.Get()}, nil
		}
}
