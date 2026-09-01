package storage

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"slices"
)

type readFS struct {
	mounts []ReadMount
}

// Open searches mounts by descending priority. Only fs.ErrNotExist falls
// through to the next mount; other errors are returned immediately.
func (r readFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	for _, mount := range r.mounts {
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

func (r readFS) mount(id MountId) (fs.FS, bool) {
	for _, mount := range r.mounts {
		if mount.Id == id {
			return mount.FS, true
		}
	}
	return nil, false
}

func (r readFS) mountsSnapshot() []ReadMount {
	return slices.Clone(r.mounts)
}

func newReadFS(mounts []ReadMount) readFS {
	result := readFS{mounts: slices.Clone(mounts)}
	slices.SortStableFunc(result.mounts, func(a, b ReadMount) int {
		return cmp.Compare(b.Priority, a.Priority)
	})
	return result
}

func (r readFS) withMount(mount ReadMount) readFS {
	mounts := slices.Clone(r.mounts)
	for i := range mounts {
		if mounts[i].Id == mount.Id {
			mounts[i] = mount
			return newReadFS(mounts)
		}
	}
	mounts = append(mounts, mount)
	return newReadFS(mounts)
}

func (r readFS) withoutMount(id MountId) readFS {
	mounts := make([]ReadMount, 0, len(r.mounts))
	for _, mount := range r.mounts {
		if mount.Id != id {
			mounts = append(mounts, mount)
		}
	}
	return readFS{mounts: mounts}
}

type writeFS interface {
	fs.FS
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(name string) error
	Rename(oldName, newName string) error
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
func (v valueStore) load(readFS ReadFS) (valueStore, error) {
	if v.entries != nil {
		return v, nil
	}
	data, err := fs.ReadFile(readFS, v.path)
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
