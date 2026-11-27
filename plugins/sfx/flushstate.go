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
