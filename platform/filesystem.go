package platform

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/dvoyni/cog/kernel"
)

// fileSystem is the FS resource: the writable filesystem and the read
// mounts stacked into one overlay, through the sockets they are bound to. It
// is an fs.ReadDirFS, fs.StatFS and fs.ReadFileFS, so the fs helpers take the
// short path, and has the read methods alone, so the resource cannot be
// asserted into writing; writableFileSystem adds the writes.
type fileSystem struct {
	writable kernel.ExclusiveSocket[WriteFSSocket]
	mounts   kernel.Socket[ReadMountSocket]
	// ordering orders the layers on first use, once the sockets resolved.
	ordering sync.Once
	// ordered is the search order: the writable filesystem, then the read
	// mounts by priority, equal priorities in plugin order.
	ordered []FS
}

// errNotDirectory is why listing a name that is a file fails.
var errNotDirectory = errors.New("not a directory")

// Open opens name in the highest layer that has it. Only a layer not having
// it passes the search on; any other failure is answered. A directory opens
// listing every layer's entries, as ReadDir does.
func (f *fileSystem) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	for _, layer := range f.layers() {
		file, err := layer.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		if !info.IsDir() {
			return file, nil
		}
		entries, err := f.ReadDir(name)
		if err != nil {
			file.Close()
			return nil, err
		}
		return &directory{File: file, entries: entries}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir lists directory name across the layers, sorted by name: the union
// of their entries, a name in several showing the highest layer's entry. The
// highest layer that has name decides whether it is a directory; a lower
// layer's file under that name is hidden, as is anything beneath it.
func (f *fileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	var entries []fs.DirEntry
	seen := map[string]bool{}
	found := false
	for _, layer := range f.layers() {
		info, err := fs.Stat(layer, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !found {
				return nil, &fs.PathError{Op: "readdir", Path: name, Err: errNotDirectory}
			}
			continue
		}
		layerEntries, err := fs.ReadDir(layer, name)
		if err != nil {
			return nil, err
		}
		found = true
		for _, entry := range layerEntries {
			if !seen[entry.Name()] {
				seen[entry.Name()] = true
				entries = append(entries, entry)
			}
		}
	}
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

// Stat describes name as the highest layer that has it does.
func (f *fileSystem) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	for _, layer := range f.layers() {
		info, err := fs.Stat(layer, name)
		if !errors.Is(err, fs.ErrNotExist) {
			return info, err
		}
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadFile reads name from the highest layer that has it.
func (f *fileSystem) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	for _, layer := range f.layers() {
		data, err := fs.ReadFile(layer, name)
		if !errors.Is(err, fs.ErrNotExist) {
			return data, err
		}
	}
	return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
}

// layers answers the search order, ordering it on first use.
func (f *fileSystem) layers() []FS {
	f.ordering.Do(func() {
		mounts := slices.Clone(f.mounts.GetAll())
		slices.SortStableFunc(mounts, func(a, b ReadMountSocket) int { return cmp.Compare(b.Priority, a.Priority) })
		f.ordered = []FS{f.writable.Get()}
		for _, mount := range mounts {
			f.ordered = append(f.ordered, mount.FS)
		}
	})
	return f.ordered
}

// writableFileSystem is the WriteFS ElevateFileAccess answers:
// fileSystem with the writes, which land in the writable filesystem.
type writableFileSystem struct{ *fileSystem }

// WriteFile writes data to name in the writable filesystem.
func (w writableFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return w.fileSystem.writable.Get().WriteFile(name, data, perm)
}

// MkdirAll creates the directory path in the writable filesystem.
func (w writableFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	return w.fileSystem.writable.Get().MkdirAll(path, perm)
}

// Remove removes name from the writable filesystem; a read mount's file of
// that name is left, and is then what name reads.
func (w writableFileSystem) Remove(name string) error {
	return w.fileSystem.writable.Get().Remove(name)
}

// Rename moves oldName to newName within the writable filesystem.
func (w writableFileSystem) Rename(oldName, newName string) error {
	return w.fileSystem.writable.Get().Rename(oldName, newName)
}

// AppendFile appends data to name in the writable filesystem.
func (w writableFileSystem) AppendFile(name string, data []byte, perm fs.FileMode) error {
	return w.fileSystem.writable.Get().AppendFile(name, data, perm)
}

// directory is a directory opened in the overlay: the highest layer's,
// listing the merged entries.
type directory struct {
	fs.File
	entries []fs.DirEntry
}

// ReadDir answers the next count entries, or every remaining one when count
// is not positive, as fs.ReadDirFile does.
func (d *directory) ReadDir(count int) ([]fs.DirEntry, error) {
	if count <= 0 {
		entries := d.entries
		d.entries = nil
		return entries, nil
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	count = min(count, len(d.entries))
	entries := d.entries[:count]
	d.entries = d.entries[count:]
	return entries, nil
}
