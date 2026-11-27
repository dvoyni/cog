package sfx

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// clipTable is what ClipTable names: what sfx knows of each clip, by path.
type clipTable struct {
	entries map[string]clipEntry
}

// makeClipTable answers a table holding no clip.
func makeClipTable() clipTable {
	return clipTable{entries: map[string]clipEntry{}}
}

// load answers the entry of the clip at path. A path the table does not hold
// is read through files and prepared by backend first, and its entry kept
// whatever came of it: installed when the prepare is done, still loading when
// it is not, and failed, its failure logged, when the read, the prepare or
// the install fails. A path the table holds is answered from it and never
// read again.
func (t clipTable) load(k *kernel.Kernel, files fs.FS, backend BackendSocket, path string) clipEntry {
	if entry, held := t.entries[path]; held {
		return entry
	}
	entry, err := prepareClip(files, backend, path)
	if err != nil {
		entry = clipEntry{state: clipFailed}
		k.Logger().LogErr(fmt.Errorf("sfx: clip %q: %w", path, err))
	}
	t.entries[path] = entry
	return entry
}

// prepareClip reads the clip at path through files, has backend prepare it,
// and installs it when the prepare is done, answering its entry: ready once
// installed, loading while the prepare is in flight.
func prepareClip(files fs.FS, backend BackendSocket, path string) (clipEntry, error) {
	encoded, err := fs.ReadFile(files, path)
	if err != nil {
		return clipEntry{}, err
	}
	prepared, done, err := backend.Prepare(path, encoded)
	if err != nil || !done || prepared == nil {
		return clipEntry{state: clipLoading}, err
	}
	id, err := backend.Install(prepared)
	if err != nil {
		return clipEntry{}, err
	}
	return clipEntry{
		state:    clipReady,
		id:       id,
		duration: time.Duration(float64(prepared.Duration()) * float64(time.Second)),
		channels: prepared.Channels(),
	}, nil
}

// clipState is how far a clip has come.
type clipState uint8

const (
	// clipLoading is a clip whose prepare is in flight.
	clipLoading clipState = iota
	// clipReady is a clip installed, its voices started from it.
	clipReady
	// clipFailed is a clip that could not be read, prepared or installed.
	clipFailed
)

// clipEntry is what the table keeps of one clip.
type clipEntry struct {
	state clipState
	// id is the ID the backend's Install minted, and duration and channels
	// the prepared clip's facts, each zero until the clip is ready.
	id       ClipID
	duration time.Duration
	channels int
}
