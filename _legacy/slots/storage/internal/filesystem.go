package internal

import (
	"cmp"
	"errors"
	"io/fs"
	"math"
	"slices"
)

// permanentMount reads the permanent filesystem through the overlay, resolving
// the Adapter on each open.
type permanentMount func() PermanentFS

func (p permanentMount) Open(name string) (fs.File, error) { return p().Open(name) }

// FileSystem is the resource value: an immutable snapshot of the overlay plus
// the permanent filesystem it writes to. permanent is also present in mounts
// under PermanentMount, and every constructor re-derives that entry, so the
// readable and writable views cannot drift apart.
//
// permanent is a function rather than the filesystem itself because the
// resource is built in Register, and the PermanentFS Adapter is bound only at
// composition, after every Register. The plugin hands in the Adapter handle's
// Get, which is valid by the time any handler reads the resource.
type FileSystem struct {
	mounts    []ReadMount
	permanent func() PermanentFS
}

var _ fs.FS = FileSystem{}

// NewFileSystem builds the overlay. It drops any caller-supplied PermanentMount
// and, when permanent is not nil, re-adds it at the head of the search order.
// It sits ahead of the sort rather than at math.MaxInt within it, because other
// mounts use MaxInt too (canvas mounts its shaders there) and a priority tie
// must not let one of them shadow data the caller just wrote.
func NewFileSystem(mounts []ReadMount, permanent func() PermanentFS) FileSystem {
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
		return FileSystem{mounts: ordered}
	}
	result := FileSystem{mounts: make([]ReadMount, 0, len(ordered)+1), permanent: permanent}
	result.mounts = append(result.mounts, ReadMount{Id: PermanentMount, Priority: math.MaxInt, FS: permanentMount(permanent)})
	result.mounts = append(result.mounts, ordered...)
	return result
}

// NewStandaloneFileSystem builds the overlay over a single mounted filesystem at
// DefaultReadPriority, with no permanent filesystem behind it.
func NewStandaloneFileSystem(id MountId, filesystem fs.FS) FileSystem {
	return NewFileSystem([]ReadMount{{Id: id, Priority: 0, FS: filesystem}}, nil)
}

// Open searches mounts by descending priority. Only fs.ErrNotExist falls
// through to the next mount; other errors are returned immediately.
func (f FileSystem) Open(name string) (fs.File, error) {
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

// FileSystemMount reports the filesystem mounted under id.
func FileSystemMount(f FileSystem, id MountId) (fs.FS, bool) {
	for _, mount := range f.mounts {
		if mount.Id == id {
			return mount.FS, true
		}
	}
	return nil, false
}

// FileSystemMounts returns the mounts in search order, as a fresh slice.
func FileSystemMounts(f FileSystem) []ReadMount { return slices.Clone(f.mounts) }

// FileSystemWithMount returns f with mount added, or replacing the mount of
// the same id.
func FileSystemWithMount(f FileSystem, mount ReadMount) FileSystem {
	mounts := slices.Clone(f.mounts)
	for i := range mounts {
		if mounts[i].Id == mount.Id {
			mounts[i] = mount
			return NewFileSystem(mounts, f.permanent)
		}
	}
	return NewFileSystem(append(mounts, mount), f.permanent)
}

// FileSystemWithoutMount returns f without the mount of the given id.
func FileSystemWithoutMount(f FileSystem, id MountId) FileSystem {
	mounts := make([]ReadMount, 0, len(f.mounts))
	for _, mount := range f.mounts {
		if mount.Id != id {
			mounts = append(mounts, mount)
		}
	}
	return NewFileSystem(mounts, f.permanent)
}
