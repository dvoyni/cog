package sfx

import (
	"io/fs"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// clipTable is what ClipTable names: what sfx knows of each clip, by path. It
// holds one private pointer, so Get copies a word.
type clipTable struct{ state *clipTableState }

// makeClipTable answers a table holding no clip.
func makeClipTable() clipTable {
	return clipTable{state: &clipTableState{entries: map[string]clipEntry{}}}
}

// load answers the entry of the clip at path. A path the table does not hold
// is read through files and handed to backend's Prepare first, and its entry
// kept whatever came of it: ready once installed, loading while the prepare
// is in flight, and failed, its failure logged once under the clip's key,
// when the read, the prepare or the install fails. A path the table holds is
// answered from it and never read again.
func (t clipTable) load(k *kernel.Kernel, files fs.FS, backend BackendSocket, path string) clipEntry {
	if entry, held := t.state.entries[path]; held {
		return entry
	}
	t.state.lastEpoch++
	entry := clipEntry{state: clipLoading, epoch: t.state.lastEpoch}
	encoded, err := fs.ReadFile(files, path)
	if err != nil {
		entry = failClip(k, path, ErrClipUnreadable{Path: path, Err: err})
	} else {
		prepared, done, err := backend.Prepare(prepareToken{path: path, epoch: entry.epoch}, encoded)
		switch {
		case err != nil:
			entry = failClip(k, path, ErrClipFailed{Path: path, Err: err})
		case done:
			entry = installClip(k, backend, path, prepared)
		}
	}
	t.state.entries[path] = entry
	return entry
}

// drain installs every prepare backend finished since the last flush, and
// fails each clip whose prepare failed. A completion whose entry has gone, or
// moved to another epoch, as a release recorded against the prepare in
// flight leaves it, is dropped with nothing called, so no ClipID is minted
// only to be destroyed. It runs first in the flush, so its voices bind or
// fail in that same flush.
func (t clipTable) drain(k *kernel.Kernel, backend BackendSocket) {
	for _, completion := range backend.TakePrepared() {
		token, isToken := completion.Token.(prepareToken)
		if !isToken {
			continue
		}
		entry, held := t.state.entries[token.path]
		if !held || entry.epoch != token.epoch || entry.state != clipLoading {
			continue
		}
		if completion.Err != nil {
			t.state.entries[token.path] = failClip(k, token.path, ErrClipFailed{Path: token.path, Err: completion.Err})
			continue
		}
		t.state.entries[token.path] = installClip(k, backend, token.path, completion.Clip)
	}
}

// installClip has backend mint the ID of prepared, the clip at path, at the
// flush, where the destroy that pairs with it can be stated, and answers its
// entry, ready with the clip's facts. A backend that prepared nothing, or
// minted the zero ID, fails the clip with ErrClipNotPrepared, and one whose
// Install failed with ErrClipFailed.
func installClip(k *kernel.Kernel, backend BackendSocket, path string, prepared PreparedClip) clipEntry {
	if prepared == nil {
		k.Logger().LogErr(ErrClipNotPrepared{Path: path})
		return clipEntry{state: clipFailed}
	}
	id, err := backend.Install(prepared)
	if err != nil {
		return failClip(k, path, ErrClipFailed{Path: path, Err: err})
	}
	if id == 0 {
		k.Logger().LogErr(ErrClipNotPrepared{Path: path})
		return clipEntry{state: clipFailed}
	}
	return clipEntry{
		state:    clipReady,
		id:       id,
		duration: time.Duration(float64(prepared.Duration()) * float64(time.Second)),
		channels: prepared.Channels(),
	}
}

// failClip logs err once under the key of the clip at path, and answers its
// entry, failed. The failure is terminal: there is no retry, and only a
// release clears it.
func failClip(k *kernel.Kernel, path string, err error) clipEntry {
	k.Logger().LogErrOnce(clipFailedLogKey+path, err)
	return clipEntry{state: clipFailed}
}

// clipTableState is the table clipTable points to.
type clipTableState struct {
	entries map[string]clipEntry
	// lastEpoch is the epoch the newest entry was made at, each entry's
	// prepare carrying its own, so a completion for an entry that has gone
	// finds none at its epoch.
	lastEpoch uint64
}

// prepareToken is the token sfx hands Prepare, which comes back with the
// completion: the path of the entry the prepare belongs to, and the epoch
// that entry held when the prepare began.
type prepareToken struct {
	path  string
	epoch uint64
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
	// epoch is the epoch the entry was made at, which its prepare's token
	// carries; zero once the clip has failed or is ready, since no completion
	// is matched against it then.
	epoch uint64
	// id is the ID the backend's Install minted, and duration and channels
	// the prepared clip's facts, each zero until the clip is ready.
	id       ClipID
	duration time.Duration
	channels int
}
