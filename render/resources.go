package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
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
	// DeliverCapture handles gfx.CapturedEvent: it publishes CapturedEvent
	// inside it, and returns the nested publication's out dependency, so gfx
	// frees the capture's picture only after every task joined into it.
	DeliverCapture kernel.Sub[gfx.CapturedEvent]
}
