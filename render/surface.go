package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
)

// Surface is the task parameter a task changes the windows' surfaces
// through, declared in its signature as kernel.Write is:
//
//	func toggleVsync(k *kernel.Kernel, vsync bool, surface render.Surface) kernel.NoResult
//
// It locks gfx's Device for write, alone, so its job waits for every job
// holding Inventory or Frame, and none of them overlaps it: a toggle that
// happens rarely. Its user names neither gfx nor its lock. The request at
// start is gfx's Config.Surface, set where the application lists gfx. It is
// valid only while the task's job runs.
type Surface struct {
	device kernel.Write[gfx.Device]
}

// Declare declares Surface's lock and answers it bound to what it guards.
func (Surface) Declare(access *kernel.Access) Surface {
	return Surface{device: access.Write[gfx.Device]()}
}

// Request replaces what every window's surface is asked to be configured
// with, the windows opened later included: each surface is configured with
// it before the next frame acquires its framebuffer.
func (s Surface) Request(request SurfaceRequest) {
	gfx.ElevateDeviceAccess(s.device).RequestSurface(gfx.SurfaceRequest{
		Format:      gfx.TextureFormat(request.Format),
		PresentMode: gfx.PresentMode(request.PresentMode),
	})
}
