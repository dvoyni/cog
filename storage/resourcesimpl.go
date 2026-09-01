package storage

import (
	"cmp"
	"errors"
	"io/fs"
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

func validatePath(operation, path string) error {
	if fs.ValidPath(path) {
		return nil
	}
	return &fs.PathError{Op: operation, Path: path, Err: fs.ErrInvalid}
}
