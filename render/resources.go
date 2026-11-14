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
}
