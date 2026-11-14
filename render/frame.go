package render

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
)

// Frame is the task parameter a recording task reaches the frame through,
// declared in its signature as kernel.Read is:
//
//	func drawScene(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult
//
// It locks render's frame state, its asset table and gfx's Device, all for
// read, so tasks holding it run together, and its user names neither gfx nor
// its locks. It is valid only while the task's job runs.
type Frame struct {
	state  kernel.Read[*frameState]
	device kernel.Read[gfx.Device]
}

// Declare declares Frame's locks and answers it bound to what they guard. The
// asset table is locked for the assets the frame's calls name.
func (Frame) Declare(access *kernel.Access) Frame {
	frame := Frame{state: access.Read[*frameState](), device: access.Read[gfx.Device]()}
	access.Read[*assetTable]()
	return frame
}

// BeginCommandList begins a command list of the frame being recorded, at
// order among the frame's other lists, for one task to fill. A list at an
// order another list of the frame has, or at render's own, math.MinInt32, is
// refused and reported once; so is a list begun while no frame is recording.
// A refused list records into nothing.
func (f Frame) BeginCommandList(label string, order Order) CommandList {
	state := f.state.Get()
	list := CommandList{frame: f}
	if order == ownOrder {
		state.report(ReportKindOrderReserved, fmt.Errorf("render: command list %q is at order %d, render's own", label, order))
		return list
	}
	gfxFrame := state.recordingFrame()
	encoder, err := f.device.Get().Encoder(gfxFrame, gfx.SubmitOrder(order))
	switch {
	case errors.Is(err, gfx.ErrOrderTaken):
		state.report(ReportKindOrderTaken, fmt.Errorf("render: command list %q is at order %d, which another list of the frame has", label, order))
	case errors.Is(err, gfx.ErrFrameFlushed):
		state.report(ReportKindListOutsideFrame, fmt.Errorf("render: command list %q is begun while no frame is recording", label))
	case errors.Is(err, gfx.ErrDeviceLost):
		// gfx logged the loss.
	case err != nil:
		state.logger.LogErr(fmt.Errorf("render: beginning command list %q: %w", label, err))
	default:
		list.gfxFrame = gfxFrame
		list.encoder = encoder
	}
	return list
}
