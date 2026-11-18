package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
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
	// TrackOpenedWindow handles platform.WindowOpenedEvent, and
	// ForgetClosedWindow platform.WindowClosedEvent: each keeps canvas's
	// account of the open windows, whose first opened is the main window,
	// the default camera's target.
	TrackOpenedWindow  kernel.Sub[platform.WindowOpenedEvent]
	ForgetClosedWindow kernel.Sub[platform.WindowClosedEvent]
	// DeliverInspection handles render.InspectedEvent: when canvas wrote
	// down the frame render inspected, for a request of its own, it puts
	// render's inspection beside canvas's, and publishes InspectedEvent for
	// it, in a batch of its own.
	DeliverInspection kernel.Sub[render.InspectedEvent]
}
