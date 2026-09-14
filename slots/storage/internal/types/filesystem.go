package types

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"path"
	"slices"

	"github.com/dvoyni/cog/kernel"
)

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

// permanentMount reads the permanent filesystem through the overlay, resolving
// the Adapter on each open.
type permanentMount func() PermanentFS

func (p permanentMount) Open(name string) (fs.File, error) { return p().Open(name) }

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

// WriteAccess returns the mutating half of the permanent filesystem behind a
// write lock on FileSystem.
func WriteAccess(handle kernel.Write[FileSystem]) WriteFS {
	return writeAccess(handle.Get())
}

// writeAccess returns the mutating half of f's permanent filesystem. The
// caller is responsible for holding the write lock f came from.
func writeAccess(f FileSystem) WriteFS {
	if f.permanent == nil {
		return WriteFS{}
	}
	return WriteFS{permanent: f.permanent()}
}

// WriteFS is the mutating half of the permanent filesystem, reachable only
// through WriteAccess. A zero value has no backend and reports ErrNoWriteAccess
// instead of panicking, so a handler holding an unbound write handle fails
// loudly rather than silently writing nowhere.
type WriteFS struct{ permanent PermanentFS }

func (w WriteFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "write", Path: name}
	}
	return w.permanent.WriteFile(name, data, perm)
}

func (w WriteFS) MkdirAll(name string, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "mkdir", Path: name}
	}
	return w.permanent.MkdirAll(name, perm)
}

func (w WriteFS) Remove(name string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "remove", Path: name}
	}
	return w.permanent.Remove(name)
}

func (w WriteFS) Rename(oldName, newName string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "rename", Path: oldName}
	}
	return w.permanent.Rename(oldName, newName)
}

// Values caches the values file. entries is nil until the first load, which
// distinguishes "not read yet" from "read and empty".
type Values struct {
	path    string
	entries map[string]json.RawMessage
	dirty   bool
}

// NewValues returns an empty, unloaded store over the values file at path.
func NewValues(path string) Values { return Values{path: path} }

// load reads the values file once. A missing file yields an empty store; a
// malformed one is reported without caching, so a later flush cannot overwrite
// data we failed to understand.
func (v Values) load(filesystem FileSystem) (Values, error) {
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
func (v Values) flush(writeFS WriteFS) (Values, error) {
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
