package storage

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"path"
	"slices"
)

// fileSystem is the resource value: an immutable snapshot of the overlay plus
// the permanent backend it writes to. permanent is also present in mounts under
// PermanentMount, and every constructor re-derives that entry, so the readable
// and writable views cannot drift apart.
type fileSystem struct {
	mounts    []ReadMount
	permanent PermanentFS
}

// Open searches mounts by descending priority. Only fs.ErrNotExist falls
// through to the next mount; other errors are returned immediately.
func (f fileSystem) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	for _, mount := range f.mounts {
		file, err := mount.FS.Open(name)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (f fileSystem) mount(id MountId) (fs.FS, bool) {
	for _, mount := range f.mounts {
		if mount.Id == id {
			return mount.FS, true
		}
	}
	return nil, false
}

func (f fileSystem) mountsSnapshot() []ReadMount {
	return slices.Clone(f.mounts)
}

// newFileSystem drops any caller-supplied PermanentMount and re-adds it from
// permanent at the head of the search order. It sits ahead of the sort rather
// than at math.MaxInt within it, because other mounts use MaxInt too (canvas
// mounts its shaders there) and a priority tie must not let one of them shadow
// data the caller just wrote.
func newFileSystem(mounts []ReadMount, permanent PermanentFS) fileSystem {
	ordered := make([]ReadMount, 0, len(mounts)+1)
	for _, mount := range mounts {
		if mount.Id != PermanentMount {
			ordered = append(ordered, mount)
		}
	}
	slices.SortStableFunc(ordered, func(a, b ReadMount) int {
		return cmp.Compare(b.Priority, a.Priority)
	})
	if permanent == nil {
		return fileSystem{mounts: ordered}
	}
	result := fileSystem{mounts: make([]ReadMount, 0, len(ordered)+1), permanent: permanent}
	result.mounts = append(result.mounts, ReadMount{Id: PermanentMount, Priority: math.MaxInt, FS: permanent})
	result.mounts = append(result.mounts, ordered...)
	return result
}

func (f fileSystem) withMount(mount ReadMount) fileSystem {
	mounts := slices.Clone(f.mounts)
	for i := range mounts {
		if mounts[i].Id == mount.Id {
			mounts[i] = mount
			return newFileSystem(mounts, f.permanent)
		}
	}
	return newFileSystem(append(mounts, mount), f.permanent)
}

func (f fileSystem) withoutMount(id MountId) fileSystem {
	mounts := make([]ReadMount, 0, len(f.mounts))
	for _, mount := range f.mounts {
		if mount.Id != id {
			mounts = append(mounts, mount)
		}
	}
	return newFileSystem(mounts, f.permanent)
}

func (f fileSystem) withPermanent(permanent PermanentFS) fileSystem {
	return newFileSystem(f.mounts, permanent)
}

// writeFS is the mutating half of the permanent filesystem, reachable only
// through WriteAccess. A zero value has no backend and reports ErrNoWriteAccess
// instead of panicking, so a handler holding an unbound write handle fails
// loudly rather than silently writing nowhere.
type writeFS struct{ permanent PermanentFS }

func (w writeFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "write", Path: name}
	}
	return w.permanent.WriteFile(name, data, perm)
}

func (w writeFS) MkdirAll(name string, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "mkdir", Path: name}
	}
	return w.permanent.MkdirAll(name, perm)
}

func (w writeFS) Remove(name string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "remove", Path: name}
	}
	return w.permanent.Remove(name)
}

func (w writeFS) Rename(oldName, newName string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "rename", Path: oldName}
	}
	return w.permanent.Rename(oldName, newName)
}

// valueStore caches the values file. entries is nil until the first load, which
// distinguishes "not read yet" from "read and empty".
type valueStore struct {
	path    string
	entries map[string]json.RawMessage
	dirty   bool
}

// load reads the values file once. A missing file yields an empty store; a
// malformed one is reported without caching, so a later flush cannot overwrite
// data we failed to understand.
func (v valueStore) load(filesystem FileSystem) (valueStore, error) {
	if v.entries != nil {
		return v, nil
	}
	data, err := fs.ReadFile(filesystem, v.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return v, err
		}
		v.entries = map[string]json.RawMessage{}
		return v, nil
	}
	entries := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return v, ErrInvalidValuesFile{Path: v.path, Err: err}
	}
	v.entries = entries
	return v, nil
}

// flush writes the cache through a temporary file and renames it over the
// values file, so an interrupted write cannot truncate the previous contents.
func (v valueStore) flush(writeFS WriteFS) (valueStore, error) {
	if !v.dirty {
		return v, nil
	}
	data, err := json.Marshal(v.entries)
	if err != nil {
		return v, err
	}
	if parent := path.Dir(v.path); parent != "." {
		if err := writeFS.MkdirAll(parent, 0o700); err != nil {
			return v, err
		}
	}
	temporary := v.path + ".tmp"
	if err := writeFS.WriteFile(temporary, data, 0o600); err != nil {
		return v, err
	}
	if err := writeFS.Rename(temporary, v.path); err != nil {
		_ = writeFS.Remove(temporary)
		return v, err
	}
	v.dirty = false
	return v, nil
}

func validatePath(operation, path string) error {
	if fs.ValidPath(path) {
		return nil
	}
	return &fs.PathError{Op: operation, Path: path, Err: fs.ErrInvalid}
}
