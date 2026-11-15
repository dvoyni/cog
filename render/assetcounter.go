package render

import "sync"

// assetCounter shares and counts the assets of one kind: one handle for each
// key, counted by its holders, until it is freed at the end of the frame its
// count reaches zero in. A loader keys its assets by what they load from,
// and the made assets of a kind are keyed by their own handles, each held
// once. Its mutex guards it, and a loader embedding it guards its own state
// with it too: hold and add are called with it held, so a loader joins them
// with that state; release and freeReleased take it. It is taken before the
// asset table's mutex, never inside it.
type assetCounter[K comparable, H comparable] struct {
	mutex sync.Mutex
	// handles is every asset held by its key, and entries each one's key and
	// count by its handle, until it is freed.
	handles map[K]H
	entries map[H]countedAsset[K]
	// releasedHandles are the assets whose count reached zero since the last
	// frame ended, freed when the frame ends unless acquired again by then.
	releasedHandles []H
}

// hold counts one more holder of key's asset and answers it; false when none
// is held. s's mutex is held, and hold panics when it is not.
func (s *assetCounter[K, H]) hold(key K) (H, bool) {
	s.requireLocked("hold")
	h, ok := s.handles[key]
	if !ok {
		return h, false
	}
	entry := s.entries[h]
	entry.holderCount++
	s.entries[h] = entry
	return h, true
}

// add holds h as key's asset, with one holder. s's mutex is held, and add
// panics when it is not.
func (s *assetCounter[K, H]) add(key K, h H) {
	s.requireLocked("add")
	if s.handles == nil {
		s.handles = map[K]H{}
		s.entries = map[H]countedAsset[K]{}
	}
	s.handles[key] = h
	s.entries[h] = countedAsset[K]{key: key, holderCount: 1}
}

// markFailed notes that h's load failed and was reported, so writing it
// again, after a device loss, reports it no more. s's mutex is held, and
// markFailed panics when it is not.
func (s *assetCounter[K, H]) markFailed(h H) {
	s.requireLocked("markFailed")
	if entry, ok := s.entries[h]; ok {
		entry.failed = true
		s.entries[h] = entry
	}
}

// release counts one holder of h fewer, and at the last has it freed when
// the frame ends, unless held again by then; false, counting nothing, when
// no holder holds h.
func (s *assetCounter[K, H]) release(h H) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, ok := s.entries[h]
	if !ok || entry.holderCount == 0 {
		return false
	}
	entry.holderCount--
	s.entries[h] = entry
	if entry.holderCount == 0 {
		s.releasedHandles = append(s.releasedHandles, h)
	}
	return true
}

// freeReleased calls remove with each asset whose count reached zero since
// the last frame ended and is zero still, forgetting its key, so its next
// acquire loads it again and reports a failure again.
func (s *assetCounter[K, H]) freeReleased(remove func(H)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, h := range s.releasedHandles {
		if entry, ok := s.entries[h]; ok && entry.holderCount == 0 {
			delete(s.handles, entry.key)
			delete(s.entries, h)
			remove(h)
		}
	}
	s.releasedHandles = s.releasedHandles[:0]
}

// requireLocked panics, naming operation, when s's mutex is free: a call
// that must hold it does not.
func (s *assetCounter[K, H]) requireLocked(operation string) {
	if s.mutex.TryLock() {
		s.mutex.Unlock()
		panic("render: an asset counter's " + operation + " is called without its mutex held")
	}
}

// countedAsset is what assetCounter keeps of one asset: its key, how many
// holders hold it, and whether its load failed and was reported.
type countedAsset[K comparable] struct {
	key         K
	holderCount int
	failed      bool
}
