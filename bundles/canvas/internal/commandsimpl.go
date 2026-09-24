package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// armDrawsCmdImpl installs the tick's one snapshot request and hands back the
// channel the result arrives on, plus the viewport the caller cannot read for
// itself. The Viewport read is the only lock it needs: the snapshot slot is
// plugin-owned and carries its own.
func (p *plugin) armDrawsCmdImpl() (kernel.Lock, kernel.Execute[ArmDrawsRequest, ArmDrawsResponse]) {
	var viewport kernel.Read[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request ArmDrawsRequest) ArmDrawsResponse {
			live, err := p.snapshots.arm(request)
			if err != nil {
				return ArmDrawsResponse{Err: err}
			}
			return ArmDrawsResponse{Done: live.done, Viewport: *viewport.Get()}
		}
}
