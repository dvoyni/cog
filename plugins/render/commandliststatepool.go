package render

import "sync"

// commandListStatePool is the pool of the states command lists keep while
// they record: each is held by one list from its beginning until the frame
// ends, with no reuse within the frame, so a call on a list or pass after
// its end touches no other list's.
type commandListStatePool struct {
	// mutex guards states, every state made, of which the first
	// heldCount are held this frame.
	mutex     sync.Mutex
	states    []*commandListState
	heldCount int
	// clears checks the passes of the states held.
	clears clearChecker
}

// acquire hands out a state no list holds this frame, empty, for a list at
// order, taking chunks of chunkSize bytes from arena, making one when the
// pool has none.
func (p *commandListStatePool) acquire(arena *temporaryArena, chunkSize uint64, order Order) *commandListState {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.heldCount == len(p.states) {
		p.states = append(p.states, &commandListState{})
	}
	state := p.states[p.heldCount]
	state.arena = arena
	state.chunkSize = chunkSize
	state.order = order
	p.heldCount++
	return state
}

// checkClears reports, with reporter, each pass of the lists holding a
// state this frame that clears a target an earlier pass of the frame
// drew into, in submit order, once for the two passes.
func (p *commandListStatePool) checkClears(reporter reporter) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.clears.check(p.states[:p.heldCount], reporter)
}

// endFrame takes back every state held this frame, emptied, freeing in
// table the temporary meshes each acquired.
func (p *commandListStatePool) endFrame(table *assetTable) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, state := range p.states[:p.heldCount] {
		for _, mesh := range state.temporaryMeshes {
			table.removeMesh(mesh)
		}
		state.reset()
	}
	p.heldCount = 0
}
