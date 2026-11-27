package sfx

import "github.com/dvoyni/cog/kernel"

// flushState is the flush's private resource: the socket of the bound
// backend, and the flush's scratch, its changes and its endings, which keep
// their capacity from one flush to the next.
type flushState struct {
	backend kernel.ExclusiveSocket[BackendSocket]
	changes FlushChanges
	endings []VoiceEndedEvent
}

// newFlushState answers the flush's state over backend, its changes made
// room for maxVoices starts, updates and stops. A flush states each slot at
// most once in each, so they never grow: the flush where the device arrives,
// restarting every live voice at once, allocates no more than any other.
func newFlushState(backend kernel.ExclusiveSocket[BackendSocket], maxVoices int) *flushState {
	return &flushState{
		backend: backend,
		changes: FlushChanges{
			Starts:  make([]VoiceStart, 0, maxVoices),
			Updates: make([]VoiceUpdate, 0, maxVoices),
			Stops:   make([]VoiceSlot, 0, maxVoices),
		},
	}
}
