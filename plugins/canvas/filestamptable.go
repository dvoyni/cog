package canvas

import (
	"io/fs"
	"maps"
	"slices"
	"sync"
)

// fileStampTable is the stamp of each image file and mesh file canvas has
// read, or whose texture it has packed, by its path, taken as it was first
// read and kept until its path is unloaded. The drawing job and any task
// holding Assets add to it, so its mutex guards stamps.
type fileStampTable struct {
	mutex  sync.Mutex
	stamps map[string]fileStamp
}

// add stamps the file at path in files unless it is stamped already,
// statting it outside the mutex: of two calls stamping it at once, the
// later's stamp is kept, either taken as it was read. It is called before the file is read, so a
// change made as it is read is seen by the next comparison.
func (t *fileStampTable) add(files fs.FS, path string) {
	t.mutex.Lock()
	_, stamped := t.stamps[path]
	t.mutex.Unlock()
	if stamped {
		return
	}
	stamp := readFileStamp(files, path)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.stamps == nil {
		t.stamps = map[string]fileStamp{}
	}
	t.stamps[path] = stamp
}

// findChangedPaths answers, sorted, the paths stamped whose files in
// files changed since they were stamped, statting each outside the mutex,
// so other calls do not wait for the stats.
func (t *fileStampTable) findChangedPaths(files fs.FS) []string {
	t.mutex.Lock()
	stamped := maps.Clone(t.stamps)
	t.mutex.Unlock()
	var changed []string
	for path, stamp := range stamped {
		if !readFileStamp(files, path).equal(stamp) {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

// forget forgets the stamp of each path Unload(unloaded) unloads, so the
// next read of its file stamps it again.
func (t *fileStampTable) forget(unloaded string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for path := range t.stamps {
		if isUnder(path, unloaded) {
			delete(t.stamps, path)
		}
	}
}
