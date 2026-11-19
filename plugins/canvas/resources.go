package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
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
	// the target of a camera naming none.
	TrackOpenedWindow  kernel.Sub[platform.WindowOpenedEvent]
	ForgetClosedWindow kernel.Sub[platform.WindowClosedEvent]
	// DeliverInspection handles render.InspectedEvent: when canvas wrote
	// down the frame render inspected, for a request of its own, it puts
	// render's inspection beside canvas's, and publishes InspectedEvent for
	// it, in a batch of its own.
	DeliverInspection kernel.Sub[render.InspectedEvent]
}

// Targets is the resource of each target canvas has drawn into, its size and
// scale as it was last drawn, which a Camera's conversions read, so a click
// converts through what the player saw: read it under
// kernel.Read[canvas.Targets]. canvas writes it once a frame, after drawing
// the frame, in a task of its own under its write lock, so a reader sees one
// account for its whole run, and a task converting clicks waits only for
// that copy. It holds its reads alone, and a target never drawn has no
// account.
type Targets interface {
	// Window answers window's size in points as it was last drawn, its
	// scale, its framebuffer's pixels to a point, and whether canvas has
	// drawn into it; the zero window answers the main window of the frame
	// last drawn, the target of a camera naming none.
	Window(window platform.Window) (size m.Vec2, scale float32, drawn bool)
	// Texture answers texture's size in pixels as it was last drawn, and
	// whether canvas has drawn into it.
	Texture(texture render.Texture) (size m.Vec2, drawn bool)
}
