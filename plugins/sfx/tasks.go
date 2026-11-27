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
// voices. In order, it applies the operations recorded, a clip named for the
// first time read and prepared there, and then the pause recorded last;
// advances every playhead by delta; hands
// the backend the flush's changes; frees the slots whose voices ended, only
// now that their stops are handed over, for the next flush's plays; and
// publishes each ending.
func flush(k *kernel.Kernel, delta time.Duration, queueHandle kernel.Write[Queue], voicesHandle kernel.Write[LiveVoices], clipsHandle kernel.Write[ClipTable], stateHandle kernel.Write[*flushState], filesHandle kernel.Read[platform.FS]) kernel.NoResult {
	queue := queueHandle.Get()
	voices := voicesHandle.Get()
	clips := clipsHandle.Get()
	state := stateHandle.Get()
	backend := state.backend.Get()
	state.changes.reset()
	state.endings = state.endings[:0]
	for _, recorded := range queue.state.operations {
		switch recorded.kind {
		case operationPlay:
			voices.start(recorded, clips.load(k, filesHandle.Get(), backend, recorded.path), &state.endings)
		case operationStop:
			voices.stop(recorded.voice, &state.endings)
		}
	}
	queue.state.operations = queue.state.operations[:0]
	if paused, recorded := queue.state.paused.Get(); recorded {
		voices.setPaused(paused)
		queue.state.paused = m.Maybe[bool]{}
	}
	voices.advance(delta, &state.endings)
	voices.collect(&state.changes)
	backend.Emit(&state.changes)
	queue.state.freeSlots = voices.appendFreeSlots(queue.state.freeSlots[:0])
	for _, ending := range state.endings {
		k.Publish(ending)
	}
	return kernel.NoResult{}
}
