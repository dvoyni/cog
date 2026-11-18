package canvas

import (
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
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

// trackOpenedWindow is Handlers.TrackOpenedWindow: after in, it schedules
// addWindowTask with the window just opened, and returns its job.
func trackOpenedWindow(ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent, addWindowTask *kernel.Task[platform.Window, kernel.NoResult]) (platform.WindowOpenedEvent, kernel.JobHandle) {
	return event, ex.Schedule(addWindowTask, event.Window, in).Handle()
}

// forgetClosedWindow is Handlers.ForgetClosedWindow: after in, it schedules
// removeWindowTask with the window just closed, and returns its job.
func forgetClosedWindow(ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent, removeWindowTask *kernel.Task[platform.Window, kernel.NoResult]) (platform.WindowClosedEvent, kernel.JobHandle) {
	return event, ex.Schedule(removeWindowTask, event.Window, in).Handle()
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

// draw draws a frame that recorded entries through the default camera, into
// the main window, while one is open and sized: one command list at the
// default camera's order, making the default material and the white texel
// at the first draw. A frame that recorded none, or with no main window,
// begins no list.
func draw(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame, inventory render.Inventory, manager kernel.Read[*platform.WindowManager], windows kernel.Read[*windowList], state kernel.Read[*drawState], rendering kernel.Write[*renderer]) kernel.NoResult {
	shapes := state.Get().shapes
	window := windows.Get().mainWindow()
	if len(shapes) == 0 || window == (platform.Window{}) {
		return kernel.NoResult{}
	}
	size := manager.Get().Size(window)
	framebufferSize := manager.Get().FramebufferSize(window)
	if size.Width <= 0 || size.Height <= 0 || framebufferSize.Width <= 0 || framebufferSize.Height <= 0 {
		return kernel.NoResult{}
	}
	canvasRenderer := rendering.Get()
	if canvasRenderer.defaultMaterial == (render.Material{}) {
		canvasRenderer.init(inventory)
	}
	list := frame.BeginCommandList("canvas", defaultCameraOrder)
	canvasRenderer.recordDefaultCamera(&list, window, makeDefaultCameraBlock(size, framebufferSize), shapes)
	list.End()
	return kernel.NoResult{}
}

// addWindow adds window, just opened, to canvas's windows.
func addWindow(k *kernel.Kernel, window platform.Window, windows kernel.Write[*windowList]) kernel.NoResult {
	opened := windows.Get()
	opened.windows = append(opened.windows, window)
	return kernel.NoResult{}
}

// removeWindow removes window, just closed, from canvas's windows.
func removeWindow(k *kernel.Kernel, window platform.Window, windows kernel.Write[*windowList]) kernel.NoResult {
	opened := windows.Get()
	opened.windows = slices.DeleteFunc(opened.windows, func(open platform.Window) bool { return open == window })
	return kernel.NoResult{}
}
