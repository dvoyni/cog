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

// setFiles keeps files as those h was loaded from, stamped as they were
// when it was, which claimChanged compares. s's mutex is held, and setFiles
// panics when it is not.
func (s *assetCounter[K, H]) setFiles(h H, files []stampedFile) {
	s.requireLocked("setFiles")
	if entry, ok := s.entries[h]; ok {
		entry.files = files
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

// claimChanged answers the assets s holds one of whose files fs.Stat in
// files answers another stamp of than s keeps, statting each path once, and
// claims each change: s keeps the new stamps, so neither a later call nor
// one running beside it claims the change again. It stats no file holding
// s's mutex.
func (s *assetCounter[K, H]) claimChanged(files fs.FS) []changedAsset[K, H] {
	s.mutex.Lock()
	assets := make([]changedAsset[K, H], 0, len(s.entries))
	for h, entry := range s.entries {
		assets = append(assets, changedAsset[K, H]{handle: h, key: entry.key, previousFiles: entry.files})
	}
	s.mutex.Unlock()
	stamps := map[string]fileStamp{}
	changed := assets[:0]
	for _, asset := range assets {
		unchanged := true
		for _, previous := range asset.previousFiles {
			stamp, ok := stamps[previous.path]
			if !ok {
				stamp = makeFileStamp(fs.Stat(files, previous.path))
				stamps[previous.path] = stamp
			}
			unchanged = unchanged && stamp.equal(previous.stamp)
		}
		if unchanged {
			continue
		}
		asset.files = make([]stampedFile, len(asset.previousFiles))
		for index, previous := range asset.previousFiles {
			asset.files[index] = stampedFile{path: previous.path, stamp: stamps[previous.path]}
		}
		changed = append(changed, asset)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	claimed := changed[:0]
	for _, asset := range changed {
		if entry, ok := s.entries[asset.handle]; ok && slices.EqualFunc(entry.files, asset.previousFiles, stampedFile.equal) {
			entry.files = asset.files
			s.entries[asset.handle] = entry
			claimed = append(claimed, asset)
		}
	}
	return claimed
}

// queueReload queues reload in reloads, which s's mutex guards, for the
// next frame to swap in, in place of one of its asset queued before, whose
// replacement release releases, and keeps the files reload read in place
// of claimedFiles, as replaceFiles does; it releases reload's own replacement
// instead, dropping reload, when s no longer holds the asset or a later
// claim took a newer change of its files.
func (s *assetCounter[K, H]) queueReload[O comparable](reloads *[]assetReload[H, O], claimedFiles []stampedFile, reload assetReload[H, O], release func(O)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	releaseReplacement := func(r assetReload[H, O]) {
		var none O
		if r.replacement != none {
			release(r.replacement)
		}
	}
	if !s.replaceFiles(reload.handle, claimedFiles, reload.files) {
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

// takeReloads takes the reloads queued in reloads, noting each asset as
// reloaded, so writing it again after a device loss reads it even when
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

// replaceFiles keeps files as those h was read from in place of
// claimedFiles, those a claim of a change to them kept, unless a later claim
// took a newer change; false when it did, or s no longer holds h. A reload
// that read other files than its asset was loaded from, as a shader whose
// includes changed, keeps those it read, and its next change is a change to
// them. s's mutex is held, and replaceFiles panics when it is not.
func (s *assetCounter[K, H]) replaceFiles(h H, claimedFiles, files []stampedFile) bool {
	s.requireLocked("replaceFiles")
	entry, ok := s.entries[h]
	if !ok || !slices.EqualFunc(entry.files, claimedFiles, stampedFile.equal) {
		return false
	}
	entry.files = files
	s.entries[h] = entry
	return true
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
// loaded one, the files it was read from, stamped as they were when it was
// or a change of them was last claimed.
type countedAsset[K comparable] struct {
	key         K
	holderCount int
	failed      bool
	files       []stampedFile
}

// assetReload is a loaded asset ReloadChanged reloaded, to be swapped in at
// the start of the next frame: its handle, the files it was read from,
// stamped, the contents read, a texture's texels or a buffer's bytes
// padded, and what replaces the object behind its handle: a gfx texture or
// buffer, none when the contents are written into the one there, or a
// shader's slot; for a texture, its size and whether it was acquired with
// Mipmaps.
type assetReload[H comparable, O comparable] struct {
	handle      H
	files       []stampedFile
	contents    []byte
	replacement O
	size        gfx.Extent3D
	mipmaps     bool
}

// changedAsset is a loaded asset one of whose files changed: its handle
// and key, and its files stamped as they were before and as they are now.
type changedAsset[K comparable, H comparable] struct {
	handle        H
	key           K
	previousFiles []stampedFile
	files         []stampedFile
}
