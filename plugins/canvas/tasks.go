package canvas

import (
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// recordFrame is Handlers.RecordFrame: after render's Ready, it schedules
// openFrameTask, opening the frame for entries, and publishes FrameEvent
// inside render.FrameEvent, with render's beat and timing signals and that
// job as its Ready; after the nested publication's out dependency, it
// schedules sortEntriesTask, closing the frame and sorting the entries of
// its draw lists, after that drawTask with the frame's beat, and after that
// copyTargetsTask. It returns copyTargetsTask's job joined with in.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent, openFrameTask, sortEntriesTask, copyTargetsTask *kernel.Task[kernel.NoArg, kernel.NoResult], drawTask *kernel.Task[metronome.Beat, kernel.NoResult]) (render.FrameEvent, kernel.JobHandle) {
	opened := ex.Schedule(openFrameTask, kernel.NoArg{}, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       opened,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	sorted := ex.Schedule(sortEntriesTask, kernel.NoArg{}, out, opened).Handle()
	drawn := ex.Schedule(drawTask, event.Beat, sorted).Handle()
	copied := ex.Schedule(copyTargetsTask, kernel.NoArg{}, drawn).Handle()
	return event, ex.Join(in, copied)
}

// deliverInspection is Handlers.DeliverInspection: after in, it schedules
// matchInspectionTask with event, and returns its job, which render's
// inspection waits for before it is dropped.
func deliverInspection(ex *kernel.Executor, in kernel.JobHandle, event render.InspectedEvent, matchInspectionTask *kernel.Task[render.InspectedEvent, kernel.NoResult]) (render.InspectedEvent, kernel.JobHandle) {
	return event, ex.Schedule(matchInspectionTask, event, in).Handle()
}

// publishInspection handles inspectionDeliveryEvent: it publishes
// InspectedEvent inside it, schedules dropInspectionTask for its beat after
// the nested publication's out dependency, and returns that job.
func publishInspection(ex *kernel.Executor, in kernel.JobHandle, event inspectionDeliveryEvent, dropInspectionTask *kernel.Task[metronome.Beat, kernel.NoResult]) (inspectionDeliveryEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, InspectedEvent{Beat: event.beat})
	return event, ex.Schedule(dropInspectionTask, event.beat, out).Handle()
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

// sortEntries closes the frame to entries, merges every task's draw list
// into the frame's entries, in registration order, and sorts them.
func sortEntries(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*drawState]) kernel.NoResult {
	state.Get().closeAndSort()
	return kernel.NoResult{}
}

// draw draws the frame of beat through its cameras, the default one for a
// frame of entries that set none, each order's cameras in a command list of
// their own, one after another, the main window the target of a camera
// naming none, a converted image read from platform's FS, then gives back
// the 2D materials released since the frame before was drawn, so the frame
// recorded as one was released still drew it. A frame
// inspected is written down for the pending request, and render is asked
// again for its inspection, so a request render joined to an inspection of
// an earlier frame still gets one.
func draw(k *kernel.Kernel, beat metronome.Beat, frame render.Frame, inventory render.Inventory, files kernel.Read[platform.FS], manager kernel.Read[*platform.WindowManager], windows kernel.Read[*windowList], state kernel.Read[*drawState], rendering kernel.Write[*renderer]) kernel.NoResult {
	canvasState := state.Get()
	drawing := rendering.Get()
	drawing.drawCameras(frame, inventory, files.Get(), manager.Get(), windows.Get().mainWindow(), canvasState)
	canvasState.materials.freeReleased(inventory)
	if canvasState.frameInspected {
		canvasState.inspector.record(canvasState.makeInspection(beat, drawing.cameraDraws))
		frame.RequestInspection()
	}
	return kernel.NoResult{}
}

// copyTargets copies into Targets each target the frame's cameras drew
// into, as they drew into it, under its write lock, so a reader sees one
// account for its whole run.
func copyTargets(k *kernel.Kernel, _ kernel.NoArg, targets kernel.Write[Targets], rendering kernel.Read[*renderer]) kernel.NoResult {
	drawing := rendering.Get()
	targets.Get().(*drawnTargets).record(drawing.mainWindow, drawing.cameraDraws)
	return kernel.NoResult{}
}

// matchInspection puts render's inspection of event's frame beside canvas's,
// when canvas wrote that frame down for its pending request, and then
// publishes inspectionDeliveryEvent for it, in a batch of its own. With no
// request pending, canvas wrote no frame down, and reads nothing.
func matchInspection(k *kernel.Kernel, event render.InspectedEvent, frame render.Frame, state kernel.Read[*drawState]) kernel.NoResult {
	if !state.Get().inspector.requestPending.Load() {
		return kernel.NoResult{}
	}
	renderInspection, err := frame.ReadInspection(event)
	if err != nil {
		state.Get().logger.LogErr(err)
		return kernel.NoResult{}
	}
	if state.Get().inspector.deliver(event.Beat, renderInspection) {
		k.Publish(inspectionDeliveryEvent{beat: event.Beat})
	}
	return kernel.NoResult{}
}

// dropInspection drops the inspection of the frame of beat, once
// InspectedEvent's jobs have run.
func dropInspection(k *kernel.Kernel, beat metronome.Beat, state kernel.Read[*drawState]) kernel.NoResult {
	state.Get().inspector.drop(beat)
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
