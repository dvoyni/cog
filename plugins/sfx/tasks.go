package sfx

import (
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
)

// flush is the body of Tasks.Flush. It holds every resource of sfx's at once,
// which is what applying a flush atomically means in lock terms: no recorder
// is mid-append while the queue is drained, and no reader sees half a flush's
// voices. In order, it installs the prepares the backend finished since the
// last flush; applies the operations recorded, a clip named for the first
// time read and prepared there, and a release ending the voices on its clip
// before destroying it, and then the bus volumes, the listener and the pause
// they coalesced to; binds the voices waiting on a clip that is now
// ready and ends those whose clip failed; advances every playhead by delta;
// computes every live voice's params, the W3C equations against the
// listener, its bus's volume and the pause; polls the backend's device into
// DeviceView; hands the backend the flush's changes, restating a voice only
// when its params differ from those the backend holds, and every voice it
// holds restarted at its playhead, with those params, when the device has
// just turned ready; frees the slots whose voices ended, only now that their
// stops are handed over, for the next flush's plays; and publishes each
// ending.
func flush(k *kernel.Kernel, delta time.Duration, queueHandle kernel.Write[Queue], voicesHandle kernel.Write[LiveVoices], clipsHandle kernel.Write[ClipTable], busesHandle kernel.Write[BusVolumes], listenerHandle kernel.Write[Listener], deviceHandle kernel.Write[DeviceView], stateHandle kernel.Write[*flushState], filesHandle kernel.Read[platform.FS]) kernel.NoResult {
	queue := queueHandle.Get()
	voices := voicesHandle.Get()
	clips := clipsHandle.Get()
	buses := busesHandle.Get()
	heardFrom := listenerHandle.Get()
	state := stateHandle.Get()
	backend := state.backend.Get()
	state.changes.reset()
	state.endings = state.endings[:0]
	clips.drain(k, backend)
	for _, recorded := range queue.state.operations {
		switch recorded.kind {
		case operationPlay:
			voices.start(recorded, clips.load(k, filesHandle.Get(), backend, recorded.path), &state.endings)
		case operationStop:
			voices.stop(recorded.voice, &state.endings)
		case operationStopBus:
			voices.stopBus(recorded.bus, &state.endings)
		case operationPreload:
			clips.load(k, filesHandle.Get(), backend, recorded.path)
		case operationRelease:
			voices.release(recorded.path, &state.endings)
			clips.release(k, recorded.path, &state.changes.Destroys)
		case operationReleaseAll:
			voices.releaseAll(&state.endings)
			clips.releaseAll(k, &state.changes.Destroys)
		}
	}
	queue.state.operations = queue.state.operations[:0]
	buses.apply(queue.state.busVolumeSets)
	clear(queue.state.busVolumeSets)
	heardFrom.apply(queue.state.listener)
	listenerHandle.Set(heardFrom)
	queue.state.listener = ListenerOptions{}
	if paused, recorded := queue.state.paused.Get(); recorded {
		voices.table.paused = paused
		queue.state.paused = m.Maybe[bool]{}
	}
	voices.resolve(clips, &state.endings)
	voices.advance(delta, &state.endings)
	voices.computeParams(heardFrom, buses)
	wasReady := deviceHandle.Get().device.Ready
	device := backend.Device()
	deviceHandle.Set(deviceView{device: device})
	if device.Ready && !wasReady {
		voices.restate()
	}
	voices.collect(&state.changes)
	backend.Emit(&state.changes)
	queue.state.freeSlots = voices.appendFreeSlots(queue.state.freeSlots[:0])
	for _, ending := range state.endings {
		k.Publish(ending)
	}
	return kernel.NoResult{}
}
