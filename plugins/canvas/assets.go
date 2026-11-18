package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// Assets is the task parameter a task reaches canvas's assets and tools
// through, declared in its signature as kernel.Read is:
//
//	func inspectOnKey(k *kernel.Kernel, event canvas.FrameEvent, assets canvas.Assets) kernel.NoResult
//
// It locks canvas's draw state for read, and, through a render.Frame it
// holds, render's frame state, its asset table, gfx's Device and platform's
// FS, all for read, so tasks holding it run together, and with recording
// tasks. It is valid only while the task's job runs.
type Assets struct {
	state kernel.Read[*drawState]
	frame render.Frame
}

// Declare declares Assets' locks and answers it bound to what they guard.
func (Assets) Declare(access *kernel.Access) Assets {
	return Assets{state: access.Read[*drawState](), frame: render.Frame{}.Declare(access)}
}

// RequestInspection asks for an inspection of the first frame whose
// FrameEvent is published after the call, and asks render for its own
// inspection of that frame, one at a time: a request while one is pending,
// asked for and not yet delivered, joins it. While a request is pending,
// canvas writes down each frame it draws, and InspectedEvent delivers the
// one render inspected, once render has delivered its own; every other
// frame pays one check, and allocates nothing for it.
func (a Assets) RequestInspection() {
	a.state.Get().inspector.requestPending.Store(true)
	a.frame.RequestInspection()
}

// ReadInspection answers event's inspection, with render's beside it. It
// answers it only while the jobs of event's publication run, so a handler
// of InspectedEvent calls it in a task it joins into the out dependency it
// returns; later, it answers an error.
func (a Assets) ReadInspection(event InspectedEvent) (Inspection, error) {
	inspection, ok := a.state.Get().inspector.read(event.Beat)
	if !ok {
		return Inspection{}, fmt.Errorf("canvas: reading the inspection of beat %d, which is read only while InspectedEvent's jobs run", event.Beat.Tick)
	}
	return inspection, nil
}
