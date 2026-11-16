package render

import (
	"io/fs"
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

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

// setStamp keeps stamp as the stamp of h's file when it was loaded, which
// claimChanged compares. s's mutex is held, and setStamp panics when it is
// not.
func (s *assetCounter[K, H]) setStamp(h H, stamp fileStamp) {
	s.requireLocked("setStamp")
	if entry, ok := s.entries[h]; ok {
		entry.stamp = stamp
		s.entries[h] = entry
	}
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

// claimChanged answers the assets s holds whose file, at the path readPath
// answers of its key, fs.Stat in files answers another stamp of than s
// keeps, and claims each change: s keeps the new stamp, so neither a later
// call nor one running beside it claims the change again. It stats no file
// holding s's mutex.
func (s *assetCounter[K, H]) claimChanged(files fs.FS, readPath func(K) string) []changedAsset[K, H] {
	s.mutex.Lock()
	assets := make([]changedAsset[K, H], 0, len(s.entries))
	for h, entry := range s.entries {
		assets = append(assets, changedAsset[K, H]{handle: h, key: entry.key, previousStamp: entry.stamp})
	}
	s.mutex.Unlock()
	changed := assets[:0]
	for _, asset := range assets {
		if asset.stamp = makeFileStamp(fs.Stat(files, readPath(asset.key))); !asset.stamp.equal(asset.previousStamp) {
			changed = append(changed, asset)
		}
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	claimed := changed[:0]
	for _, asset := range changed {
		if entry, ok := s.entries[asset.handle]; ok && entry.stamp.equal(asset.previousStamp) {
			entry.stamp = asset.stamp
			s.entries[asset.handle] = entry
			claimed = append(claimed, asset)
		}
	}
	return claimed
}

// queueReload queues reload in reloads, which s's mutex guards, for the
// next frame to swap in, in place of one of its asset queued before, whose
// replacement release releases; it releases reload's own instead, dropping
// reload, when s no longer holds the asset or a later claim took a newer
// change of its file.
func (s *assetCounter[K, H]) queueReload[O comparable](reloads *[]assetReload[H, O], reload assetReload[H, O], release func(O)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	releaseReplacement := func(r assetReload[H, O]) {
		var none O
		if r.replacement != none {
			release(r.replacement)
		}
	}
	if entry, ok := s.entries[reload.handle]; !ok || !entry.stamp.equal(reload.stamp) {
		releaseReplacement(reload)
		return
	}
	index := slices.IndexFunc(*reloads, func(queued assetReload[H, O]) bool { return queued.handle == reload.handle })
	if index < 0 {
		*reloads = append(*reloads, reload)
		return
	}
	releaseReplacement((*reloads)[index])
	(*reloads)[index] = reload
}

// takeReloads takes the reloads queued in reloads, noting each asset's file
// as reloaded, so writing it again after a device loss reads it even when
// its load had failed.
func (s *assetCounter[K, H]) takeReloads[O comparable](reloads *[]assetReload[H, O]) []assetReload[H, O] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	taken := *reloads
	*reloads = nil
	for _, reload := range taken {
		if entry, ok := s.entries[reload.handle]; ok {
			entry.failed = false
			s.entries[reload.handle] = entry
		}
	}
	return taken
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
// holders hold it, whether its load failed and was reported, and, for a
// loaded one, the stamp of its file when it was loaded or a change of it
// last claimed.
type countedAsset[K comparable] struct {
	key         K
	holderCount int
	failed      bool
	stamp       fileStamp
}

// assetReload is a loaded asset ReloadChanged reloaded, to be swapped in at
// the start of the next frame: its handle, the stamp of the file it was
// read from, the contents read, a texture's texels or a buffer's bytes
// padded, and the gfx object made to replace the one behind its handle,
// none when the contents are written into that one; for a texture, its
// size and whether it was acquired with Mipmaps.
type assetReload[H comparable, O comparable] struct {
	handle      H
	stamp       fileStamp
	contents    []byte
	replacement O
	size        gfx.Extent3D
	mipmaps     bool
}

// changedAsset is a loaded asset whose file changed: its handle and key,
// and the stamp of its file before and now.
type changedAsset[K comparable, H comparable] struct {
	handle        H
	key           K
	previousStamp fileStamp
	stamp         fileStamp
}
