package render

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
)

// Handlers is the resource holding every event handler render subscribes, so
// a plugin listed after render orders its own around them with
// configurator.OrderSubscriptions.
type Handlers struct {
	// RecordFrame handles gfx.DrawEvent: it publishes FrameEvent inside it,
	// and returns the nested publication's out dependency, so gfx flushes
	// the frame only after every recording task joined into it.
	RecordFrame kernel.Sub[gfx.DrawEvent]
	// EndFrame handles gfx.RenderEvent: after gfx's present, which is the
	// out dependency of gfx's own handler before it, it ends the frame,
	// taking back the temporaries and freeing those gone unused, and returns
	// that job, so the next beat starts after it.
	EndFrame kernel.Sub[gfx.RenderEvent]
	// ReloadAssets handles gfx.DeviceRecoveredEvent: it schedules the job
	// writing render's assets again, publishes RecoveredEvent inside it after
	// that job, and returns the nested publication's out dependency.
	ReloadAssets kernel.Sub[gfx.DeviceRecoveredEvent]
	// DeliverCapture handles gfx.CapturedEvent: it publishes CapturedEvent
	// inside it, and returns the nested publication's out dependency, so gfx
	// frees the capture's picture only after every task joined into it.
	DeliverCapture kernel.Sub[gfx.CapturedEvent]
	// DeliverReadback handles gfx.BufferMappedEvent: it schedules the job
	// that finds the readback whose staging buffer the event names, which
	// publishes ReadbackEvent, or PassTimingsEvent for the readback of a
	// timed frame's timestamps, in a publication of its own, and returns
	// that job.
	DeliverReadback kernel.Sub[gfx.BufferMappedEvent]
}
