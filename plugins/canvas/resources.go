package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// Handlers is the resource holding every event handler canvas subscribes, so
// a plugin listed after canvas orders its own around them with
// configurator.OrderSubscriptions.
type Handlers struct {
	// RecordFrame handles render.FrameEvent: it publishes FrameEvent inside
	// it, draws the frame after the nested publication's out dependency, and
	// returns that drawing joined with its in dependency, so render's
	// second pickup of loads and gfx's flush wait for it.
	RecordFrame kernel.Sub[render.FrameEvent]
}
