package anim

// timelines holds every timeline by key. Keys are compared as map keys, so a
// private marker struct per owner keeps them from colliding across packages.
type timelines struct {
	byKey map[any]*Timeline
}

func newTimelines() *timelines {
	return &timelines{byKey: map[any]*Timeline{}}
}

// Get returns the timeline stored under key, creating an empty one on demand.
// It mutates the resource, so it needs a write lock; readers use Lookup.
func (t *timelines) Get(key any) *Timeline {
	tl := t.byKey[key]
	if tl == nil {
		tl = &Timeline{}
		t.byKey[key] = tl
	}
	return tl
}

// Lookup returns the timeline stored under key, or nil (the no-op timeline)
// when there is none. It never creates, so it is safe under a read lock.
func (t *timelines) Lookup(key any) *Timeline {
	return t.byKey[key]
}

// Reset clears the timeline under key in place: its clock returns to zero and
// its tracks and cues are dropped. A pointer obtained earlier stays valid. An
// unknown key is a no-op.
func (t *timelines) Reset(key any) {
	if tl := t.byKey[key]; tl != nil {
		tl.Reset()
	}
}

// Delete removes the timeline under key. A pointer obtained earlier is
// detached: it keeps working but is never advanced again.
func (t *timelines) Delete(key any) {
	delete(t.byKey, key)
}

// advance steps every timeline by dt seconds.
func (t *timelines) advance(dt float32) {
	for _, tl := range t.byKey {
		tl.advance(dt)
	}
}
