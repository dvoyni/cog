package sfx

import (
	"errors"
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

// Info answers what sfx knows of the clip at path, its facts and how far it
// has come, every fact zero until the clip is ready. A path nothing has named
// answers zero facts and ClipLoading, as one whose prepare is in flight does,
// and so does a path released since: nothing failed and nothing is resident.
// Asking never starts a load, since it reads the table alone: a game that
// only asks is told ClipLoading forever, and Preload is the verb that changes
// the answer.
func (t clipTable) Info(path string) (ClipInfo, ClipState) {
	entry := t.state.entries[path]
	return entry.info, entry.state
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
	entry := clipEntry{state: ClipLoading, epoch: t.state.lastEpoch}
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
		if !held || entry.epoch != token.epoch || entry.state != ClipLoading {
			continue
		}
		if completion.Err != nil {
			t.state.entries[token.path] = failClip(k, token.path, ErrClipFailed{Path: token.path, Err: completion.Err})
			continue
		}
		t.state.entries[token.path] = installClip(k, backend, token.path, completion.Clip)
	}
}

// release takes the clip at path out of the table, after the flush has ended
// the voices playing it: its ID, when it was installed, is appended to
// destroys, the flush's changes, which a backend applies after their stops,
// and its reports are forgotten, its failure or its dropped loop region, so a
// path that failed and is named again is read again and reported again if it
// fails again. A clip whose prepare is in flight leaves nothing to destroy:
// its completion finds no entry at its epoch and is dropped. A path the table
// does not hold is left as it is.
func (t clipTable) release(k *kernel.Kernel, path string, destroys *[]ClipID) {
	entry, held := t.state.entries[path]
	if !held {
		return
	}
	delete(t.state.entries, path)
	switch entry.state {
	case ClipReady:
		*destroys = append(*destroys, entry.id)
		k.Logger().ForgetLogOnce(loopRegionIgnoredLogKey + path)
	case ClipFailed:
		k.Logger().ForgetLogOnce(clipFailedLogKey + path)
	}
}

// releaseAll releases every clip the table holds, as release does each.
func (t clipTable) releaseAll(k *kernel.Kernel, destroys *[]ClipID) {
	for path := range t.state.entries {
		t.release(k, path, destroys)
	}
}

// installClip has backend mint the ID of prepared, the clip at path, at the
// flush, where the destroy that pairs with it can be stated, and answers its
// entry, ready with the clip's facts and its bounds, in which the span a
// looping voice repeats between is its loop region, or the whole clip when it
// declares none or one the clip does not contain. That last is not distrust of
// the backend, which drops a malformed region itself; it is that the wrap
// subtracts a span, and a span outside the clip is a loop with no reason to
// stop. A backend that
// prepared nothing, or minted the zero ID, fails the clip with
// ErrClipNotPrepared, and one whose Install failed with ErrClipFailed. A loop
// region the backend dropped is logged once under the clip's loop key, as the
// ErrLoopRegionIgnored its LoopRegionErr answered with the path filled in, and
// the clip loops whole: it prepares and plays, and this is the only notice
// that it plays without the loop point its author wrote.
func installClip(k *kernel.Kernel, backend BackendSocket, path string, prepared PreparedClip) clipEntry {
	if prepared == nil {
		k.Logger().LogErr(ErrClipNotPrepared{Path: path})
		return clipEntry{state: ClipFailed}
	}
	id, err := backend.Install(prepared)
	if err != nil {
		return failClip(k, path, ErrClipFailed{Path: path, Err: err})
	}
	if id == 0 {
		k.Logger().LogErr(ErrClipNotPrepared{Path: path})
		return clipEntry{state: ClipFailed}
	}
	if err := prepared.LoopRegionErr(); err != nil {
		ignored, isIgnored := errors.AsType[ErrLoopRegionIgnored](err)
		if !isIgnored {
			ignored = ErrLoopRegionIgnored{Err: err}
		}
		ignored.Path = path
		k.Logger().LogErrOnce(loopRegionIgnoredLogKey+path, ignored)
	}
	duration := convertSecondsToDuration(prepared.Duration())
	entry := clipEntry{
		state: ClipReady,
		id:    id,
		info: ClipInfo{
			Duration:   duration,
			Channels:   prepared.Channels(),
			SampleRate: prepared.SampleRate(),
		},
		bounds: clipBounds{duration: duration, loopEnd: duration},
	}
	if region, declared := prepared.LoopRegion().Get(); declared {
		start := convertSecondsToDuration(region.Start)
		end := convertSecondsToDuration(region.End)
		if start >= 0 && end > start && end <= duration {
			entry.bounds.loopStart = start
			entry.bounds.loopEnd = end
		}
	}
	return entry
}

// failClip logs err once under the key of the clip at path, and answers its
// entry, failed. The failure is terminal: there is no retry, and only a
// release clears it.
func failClip(k *kernel.Kernel, path string, err error) clipEntry {
	k.Logger().LogErrOnce(clipFailedLogKey+path, err)
	return clipEntry{state: ClipFailed}
}

// convertSecondsToDuration answers seconds, as the seam carries a clip's
// duration and loop region, as the time.Duration a game gives and reads.
func convertSecondsToDuration(seconds float32) time.Duration {
	return time.Duration(float64(seconds) * float64(time.Second))
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

// clipEntry is what the table keeps of one clip.
type clipEntry struct {
	state ClipState
	// epoch is the epoch the entry was made at, which its prepare's token
	// carries; zero once the clip has failed or is ready, since no completion
	// is matched against it then.
	epoch uint64
	// id is the ID the backend's Install minted, info the prepared clip's
	// facts, and bounds what a voice on it is fitted to, each zero until the
	// clip is ready.
	id     ClipID
	info   ClipInfo
	bounds clipBounds
}
