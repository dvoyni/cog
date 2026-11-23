package render

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
)

// recordFrame is Handlers.RecordFrame: it schedules writeLoadsTask for the
// loads pending since the last frame, to run beside the recording; after
// gfx's acquire, it schedules beginFrameTask, which keeps the draw event's
// frame as the one being recorded and begins its inspection when one is
// requested and its timing while pass timings are on, and publishes
// FrameEvent inside
// gfx.DrawEvent, with the draw event's beat and timing signals and that job
// as its Ready; and after the nested publication's out dependency and the
// first writeLoadsTask, it schedules writeLoadsTask again, for the loads
// acquired while the frame recorded, and after that generateMipsTask, for
// the mips of every texture the two wrote, and after the mips
// readTimestampsBackTask, for the timestamps of the frame's passes when it
// is timed; and after the out dependency alone, checkClearsTask, the
// clear-after-draw check. It returns those jobs joined with beginFrameTask's,
// so gfx's flush waits for every recording task the users joined into the
// out dependency, for every load's body and its mips, for the commands
// reading the timestamps back, and for the check.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent, beginFrameTask *kernel.Task[gfx.DrawEvent, kernel.NoResult], writeLoadsTask *kernel.Task[kernel.NoArg, kernel.NoResult], generateMipsTask *kernel.Task[gfx.Frame, kernel.NoResult], readTimestampsBackTask *kernel.Task[gfx.Frame, kernel.NoResult], checkClearsTask *kernel.Task[kernel.NoArg, kernel.NoResult]) (gfx.DrawEvent, kernel.JobHandle) {
	pendingWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}).Handle()
	beginFrameJob := ex.Schedule(beginFrameTask, event, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       beginFrameJob,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	recordedWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}, out, pendingWritten).Handle()
	mipsGenerated := ex.Schedule(generateMipsTask, event.Frame, recordedWritten).Handle()
	timestampsRead := ex.Schedule(readTimestampsBackTask, event.Frame, mipsGenerated).Handle()
	clearsChecked := ex.Schedule(checkClearsTask, kernel.NoArg{}, out).Handle()
	return event, ex.Join(timestampsRead, beginFrameJob, clearsChecked)
}

// reloadAssets is Handlers.ReloadAssets: it schedules rewriteAssetsTask, and
// publishes RecoveredEvent inside event after that job, so the makers'
// tasks run after render's own assets are written again; it returns the
// nested publication's out dependency.
func reloadAssets(ex *kernel.Executor, in kernel.JobHandle, event gfx.DeviceRecoveredEvent, rewriteAssetsTask *kernel.Task[kernel.NoArg, kernel.NoResult]) (gfx.DeviceRecoveredEvent, kernel.JobHandle) {
	rewritten := ex.Schedule(rewriteAssetsTask, kernel.NoArg{}, in).Handle()
	_, out := ex.Publish(rewritten, RecoveredEvent{})
	return event, out
}

// deliverCapture is Handlers.DeliverCapture: it publishes event as render's
// CapturedEvent, keeping gfx's in it for Frame.ReadCapture, and returns the
// nested publication's out dependency, after which gfx frees the picture.
func deliverCapture(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.CapturedEvent) (gfx.CapturedEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, CapturedEvent{Window: event.Window, Beat: event.Beat, Status: CaptureStatus(event.Status), captured: event})
	return event, out
}

// publishInspection handles inspectionDeliveryEvent: it publishes
// InspectedEvent inside it, schedules dropInspectionTask after the nested
// publication's out dependency, and returns that job.
func publishInspection(ex *kernel.Executor, in kernel.JobHandle, event inspectionDeliveryEvent, dropInspectionTask *kernel.Task[metronome.Beat, kernel.NoResult]) (inspectionDeliveryEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, InspectedEvent{Beat: event.beat})
	return event, ex.Schedule(dropInspectionTask, event.beat, out).Handle()
}

// publishPassTimings handles passTimingsDeliveryEvent: it publishes
// PassTimingsEvent inside it, schedules dropPassTimingsTask for its beat
// after the nested publication's out dependency, and returns that job.
func publishPassTimings(ex *kernel.Executor, in kernel.JobHandle, event passTimingsDeliveryEvent, dropPassTimingsTask *kernel.Task[metronome.Beat, kernel.NoResult]) (passTimingsDeliveryEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, PassTimingsEvent{Beat: event.beat})
	return event, ex.Schedule(dropPassTimingsTask, event.beat, out).Handle()
}

// publishReadback handles readbackDeliveryEvent: it publishes ReadbackEvent
// inside it, schedules releaseReadbackTask for the readback after the nested
// publication's out dependency, and returns that job.
func publishReadback(ex *kernel.Executor, in kernel.JobHandle, event readbackDeliveryEvent, releaseReadbackTask *kernel.Task[Readback, kernel.NoResult]) (readbackDeliveryEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, event.delivered)
	return event, ex.Schedule(releaseReadbackTask, event.delivered.Readback, out).Handle()
}

// beginFrame keeps event's frame as the one being recorded, for Frame's
// calls, begins its inspection, of event's beat, when one is requested, and
// its timing while pass timings are on, and swaps in the textures and
// buffers reloaded since the last frame began, requesting the mips of those
// with Mipmaps.
func beginFrame(k *kernel.Kernel, event gfx.DrawEvent, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	frameState.setRecordingFrame(event.Frame)
	frameState.inspector.begin(event.Beat)
	frameState.passTimer.begin(event.Beat, device.Get(), frameState.reporter)
	frameState.mips.request(table.Get().swapReloads(device.Get())...)
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

// rewriteAssets writes render's assets again after gfx recovered a lost
// device, which made every object again under its handle, empty: it reads
// the file of each loaded buffer and decodes the body of each loaded
// texture again, and writes them, as writeLoads writes a load, taking the
// loads pending with them, and requests the mips of the textures loaded
// with Mipmaps for the next frame; and it fills the default textures and
// every material's uniform blocks again.
func rewriteAssets(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	table.Get().loadedBuffers.writeHeld(table.Get(), device.Get(), files.Get())
	state.Get().mips.request(table.Get().loadedTextures.writeHeld(table.Get(), device.Get(), files.Get())...)
	table.Get().refillDefaultTexturesAndUniforms(device.Get())
	return kernel.NoResult{}
}

// generateMips generates, in render's own command list of frame, the mips of
// every texture requested since it last ran, so gfx's submit carries them
// before every user's list.
func generateMips(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	state.Get().mips.generate(table.Get(), device.Get(), files.Get(), frame, &state.Get().passTimer)
	return kernel.NoResult{}
}

// readTimestampsBack records, in render's closing command list of frame,
// the commands reading the timestamps of the frame's passes back, when it is
// timed, and has the frame's inspection, if any, wait for them.
func readTimestampsBack(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	if frameState.passTimer.readBack(device.Get(), frame, &frameState.staging, frameState.reporter) {
		frameState.inspector.awaitPassTimings()
	}
	return kernel.NoResult{}
}

// checkClears reports each pass of the frame recorded that clears a
// target an earlier pass of the frame drew into, once for the two passes.
func checkClears(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState]) kernel.NoResult {
	frameState := state.Get()
	frameState.listStates.checkClears(frameState.reporter)
	return kernel.NoResult{}
}

// findReadback delivers, as frameState.deliverReadback does, the readback
// whose staging buffer event says gfx has mapped or failed to map; nothing
// for a buffer that is no staging buffer of render's.
func findReadback(k *kernel.Kernel, event gfx.BufferMappedEvent, state kernel.Read[*frameState], device kernel.Read[gfx.Device]) kernel.NoResult {
	status := ReadbackStatus(event.Status)
	if readback, ok := state.Get().staging.resolveMap(event.Buffer, status); ok {
		state.Get().deliverReadback(k, readback, status, device.Get())
	}
	return kernel.NoResult{}
}

// dropInspection drops the inspection of the frame of beat, once
// InspectedEvent's jobs have run.
func dropInspection(k *kernel.Kernel, beat metronome.Beat, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().inspector.drop(beat)
	return kernel.NoResult{}
}

// dropPassTimings frees the pass timings of the frame of beat, once
// PassTimingsEvent's jobs have run.
func dropPassTimings(k *kernel.Kernel, beat metronome.Beat, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().passTimer.drop(beat)
	return kernel.NoResult{}
}

// releaseReadback ends readback once its publication's jobs have run: its
// staging buffer is unmapped and goes back to the pool. An unmap that fails
// is logged, but for a device loss, which gfx has logged.
func releaseReadback(k *kernel.Kernel, readback Readback, state kernel.Read[*frameState], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	if err := frameState.staging.release(readback, device.Get()); err != nil {
		frameState.logUnlessLost(fmt.Errorf("render: unmapping the staging buffer of readback %v: %w", readback, err))
	}
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
// gone unused as long. First, gfx having submitted the frame, it starts the
// map of the staging buffer of each readback the frame recorded, publishing
// readbackDeliveryEvent, failed, for each whose map fails to start; and with
// the temporaries it frees the staging buffers no readback has held for as
// long, as it does the timed frames of pass timings. Last, it finishes the
// frame's inspection, if any, publishing inspectionDeliveryEvent for it,
// unless it waits for the frame's pass timings; and then delivers, failed,
// each readback whose map failed to start.
func endFrame(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	failedReadbacks := frameState.staging.mapRecorded(device.Get(), frameState.reporter)
	table.Get().freeReleased(device.Get())
	table.Get().endMaterialVersions(device.Get())
	frameState.mips.endFrame(device.Get())
	frameState.temporaryTextures.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	frameState.arena.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	frameState.staging.endFrame(frameState.temporaryIdleFrames, device.Get())
	frameState.passTimer.endFrame(frameState.temporaryIdleFrames, device.Get())
	frameState.listStates.endFrame(table.Get())
	table.Get().unbindFreed(device.Get())
	table.Get().layouts.freeIdleBindGroups(device.Get(), frameState.temporaryIdleFrames)
	table.Get().labels.endFrame(frameState.temporaryIdleFrames)
	if beat, ok := frameState.inspector.finish(); ok {
		k.Publish(inspectionDeliveryEvent{beat: beat})
	}
	for _, readback := range failedReadbacks {
		frameState.deliverReadback(k, readback, ReadbackStatusFailed, device.Get())
	}
	return kernel.NoResult{}
}
