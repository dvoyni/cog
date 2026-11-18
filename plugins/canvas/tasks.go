package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// defaultCameraOrder is the order of the default camera's command list, the
// camera a frame recording entries and setting none is seen through.
const defaultCameraOrder render.Order = 0

// recordFrame is Handlers.RecordFrame: after render's Ready, it schedules
// openFrameTask, opening the frame for entries, and publishes FrameEvent
// inside render.FrameEvent, with render's beat and timing signals and that
// job as its Ready; after the nested publication's out dependency, it
// schedules mergeLanesTask, closing the frame and merging its draw lists, and
// after that drawTask. It returns drawTask's job joined with in.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent, openFrameTask, mergeLanesTask, drawTask *kernel.Task[kernel.NoArg, kernel.NoResult]) (render.FrameEvent, kernel.JobHandle) {
	opened := ex.Schedule(openFrameTask, kernel.NoArg{}, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       opened,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	merged := ex.Schedule(mergeLanesTask, kernel.NoArg{}, out, opened).Handle()
	drawn := ex.Schedule(drawTask, kernel.NoArg{}, merged).Handle()
	return event, ex.Join(in, drawn)
}

// openFrame opens the frame for entries.
func openFrame(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*drawState]) kernel.NoResult {
	state.Get().recording.Store(true)
	return kernel.NoResult{}
}

// mergeLanes closes the frame to entries, and merges every task's draw list
// into the frame's entries, in registration order.
func mergeLanes(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*drawState]) kernel.NoResult {
	state.Get().closeAndMerge()
	return kernel.NoResult{}
}

// draw begins the default camera's command list when the frame recorded
// entries, and nothing when it recorded none: it draws nothing into the list
// yet.
func draw(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame, state kernel.Read[*drawState]) kernel.NoResult {
	if len(state.Get().shapes) == 0 {
		return kernel.NoResult{}
	}
	list := frame.BeginCommandList("canvas", defaultCameraOrder)
	list.End()
	return kernel.NoResult{}
}
