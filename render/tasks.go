package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// recordFrame is Handlers.RecordFrame: it schedules writeLoadsTask for the
// loads pending since the last frame, to run beside the recording; after
// gfx's acquire, it schedules beginFrameTask, which keeps the draw event's
// frame as the one being recorded, and publishes FrameEvent inside
// gfx.DrawEvent, with the draw event's beat and timing signals and that job
// as its Ready; and after the nested publication's out dependency and the
// first writeLoadsTask, it schedules writeLoadsTask again, for the loads
// acquired while the frame recorded, and after that generateMipsTask, for
// the mips of every texture the two wrote; and after the out dependency
// alone, checkClearsTask, the clear-after-draw check. It returns those jobs
// joined with beginFrameTask's, so gfx's flush waits for every recording
// task the users joined into the out dependency, for every load's body and
// its mips, and for the check.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent, beginFrameTask *kernel.Task[gfx.Frame, kernel.NoResult], writeLoadsTask *kernel.Task[kernel.NoArg, kernel.NoResult], generateMipsTask *kernel.Task[gfx.Frame, kernel.NoResult], checkClearsTask *kernel.Task[kernel.NoArg, kernel.NoResult]) (gfx.DrawEvent, kernel.JobHandle) {
	pendingWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}).Handle()
	beginFrameJob := ex.Schedule(beginFrameTask, event.Frame, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       beginFrameJob,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	recordedWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}, out, pendingWritten).Handle()
	mipsGenerated := ex.Schedule(generateMipsTask, event.Frame, recordedWritten).Handle()
	clearsChecked := ex.Schedule(checkClearsTask, kernel.NoArg{}, out).Handle()
	return event, ex.Join(mipsGenerated, beginFrameJob, clearsChecked)
}

// deliverCapture is Handlers.DeliverCapture: it publishes event as render's
// CapturedEvent, keeping gfx's in it for Frame.ReadCapture, and returns the
// nested publication's out dependency, after which gfx frees the picture.
func deliverCapture(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.CapturedEvent) (gfx.CapturedEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, CapturedEvent{Window: event.Window, Beat: event.Beat, Status: CaptureStatus(event.Status), captured: event})
	return event, out
}

// beginFrame keeps frame as the one being recorded, for Frame's calls.
func beginFrame(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().setRecordingFrame(frame)
	return kernel.NoResult{}
}

// writeLoads reads the file of each buffer loaded and not yet written, and
// decodes the body of each such texture, one after another, and writes it,
// so gfx's next submit carries it, and requests the mips of the textures
// loaded with Mipmaps.
func writeLoads(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	table.Get().loadedBuffers.writePending(table.Get(), device.Get(), files.Get())
	state.Get().mips.request(table.Get().loadedTextures.writePending(table.Get(), device.Get(), files.Get())...)
	return kernel.NoResult{}
}

// generateMips generates, in render's own command list of frame, the mips of
// every texture requested since it last ran, so gfx's submit carries them
// before every user's list.
func generateMips(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	state.Get().mips.generate(table.Get(), device.Get(), files.Get(), frame)
	return kernel.NoResult{}
}

// checkClears reports each pass of the frame recorded that clears a
// target an earlier pass of the frame drew into, once for the two passes.
func checkClears(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState]) kernel.NoResult {
	frameState := state.Get()
	frameState.listStates.checkClears(frameState.reporter)
	return kernel.NoResult{}
}

// endFrame ends the frame gfx has presented: it frees the loaded textures
// and buffers whose last acquire was released and the made ones released,
// frees the material versions the frame made, its materials' buffers taking
// their latest uniform blocks, releases the bind groups its mips drew with, takes back the temporary
// textures, the arena's chunks and the lists' states held, with the
// temporary meshes the lists acquired, frees the
// temporaries gone unused for Config's TemporaryIdleFrames frames, then
// unbinds every material value, and releases every bind group of pass or
// draw parameters, naming an asset it freed, and releases those bind groups
// gone unused as long.
func endFrame(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	table.Get().freeReleased(device.Get())
	table.Get().endMaterialVersions(device.Get())
	frameState.mips.endFrame(device.Get())
	frameState.temporaryTextures.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	frameState.arena.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	frameState.listStates.endFrame(table.Get())
	table.Get().unbindFreed(device.Get())
	table.Get().layouts.freeIdleBindGroups(device.Get(), frameState.temporaryIdleFrames)
	return kernel.NoResult{}
}
