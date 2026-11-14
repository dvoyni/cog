package filesystems

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sync"
	"time"
)

// errNotDirectory is why a file cannot be listed, or have a file put
// beneath it.
var errNotDirectory = errors.New("not a directory")

// MemoryFS is a memory mount: a read mount held in memory, for bytes that
// are not files. Its owner binds it to platform's ReadMountSocket at a
// priority, keeps it as a resource of its own, and puts files into it and
// removes them at run time under the mount's own lock, so filling it takes
// no FS write lock. Every plugin reads its files through platform's FS.
// Nothing in it is persisted. A directory is there while a file is beneath
// it.
type MemoryFS struct {
	lock  sync.RWMutex
	files map[string]putFile
	// directoryEntries is each directory's entry names, its files' and its
	// subdirectories', the root's under ".".
	directoryEntries map[string]map[string]struct{}
	// lastPutAt is the modification time the last put stamped.
	lastPutAt time.Time
}

// NewMemoryFS answers an empty memory mount.
func NewMemoryFS() *MemoryFS {
	return &MemoryFS{
		files:            map[string]putFile{},
		directoryEntries: map[string]map[string]struct{}{".": {}},
	}
}

// Open opens name for reading: a file as it was put, a directory listing
// its entries as of the open.
func (m *MemoryFS) Open(name string) (fs.File, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	info, data, err := m.describe("open", name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &openDirectory{info: info, entries: m.listEntries(name)}, nil
	}
	return &openFile{Reader: bytes.NewReader(data), info: info}, nil
}

// ReadFile reads name whole, into bytes of the caller's own.
func (m *MemoryFS) ReadFile(name string) ([]byte, error) {
	m.lock.RLock()
	info, data, err := m.describe("readfile", name)
	m.lock.RUnlock()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: errIsDirectory}
	}
	return bytes.Clone(data), nil
}

// ReadDir lists the directory name, sorted by name.
func (m *MemoryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	info, _, err := m.describe("readdir", name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errNotDirectory}
	}
	return m.listEntries(name), nil
}

// Stat describes name: a file's size, and when it was put as its
// modification time.
func (m *MemoryFS) Stat(name string) (fs.FileInfo, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	info, _, err := m.describe("stat", name)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Put puts a copy of data in as the file name, replacing the file there and
// making its directories as needed, and stamps it modified now: strictly
// later than every put before it, even within one tick of the clock, so a
// reader comparing modification times sees each put. It fails for a name
// fs.ValidPath rejects, a directory's name, the root's among them, and a name
// beneath a file.
func (m *MemoryFS) Put(name string, data []byte) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "put", Path: name, Err: fs.ErrInvalid}
	}
	file := putFile{data: bytes.Clone(data)}
	m.lock.Lock()
	defer m.lock.Unlock()
	if _, found := m.directoryEntries[name]; found {
		return &fs.PathError{Op: "put", Path: name, Err: errIsDirectory}
	}
	for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
		if _, found := m.files[directory]; found {
			return &fs.PathError{Op: "put", Path: name, Err: errNotDirectory}
		}
	}
	file.putAt = m.stampPut()
	m.files[name] = file
	for entry := name; ; entry = path.Dir(entry) {
		directory := path.Dir(entry)
		if entries, found := m.directoryEntries[directory]; found {
			entries[path.Base(entry)] = struct{}{}
			return nil
		}
		m.directoryEntries[directory] = map[string]struct{}{path.Base(entry): {}}
	}
}

// Remove removes the file name, and each directory it leaves empty. It fails
// for a name that is no file put.
func (m *MemoryFS) Remove(name string) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrInvalid}
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	if _, found := m.directoryEntries[name]; found {
		return &fs.PathError{Op: "remove", Path: name, Err: errIsDirectory}
	}
	if _, found := m.files[name]; !found {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	delete(m.files, name)
	for entry := name; ; entry = path.Dir(entry) {
		directory := path.Dir(entry)
		entries := m.directoryEntries[directory]
		delete(entries, path.Base(entry))
		if len(entries) > 0 || directory == "." {
			return nil
		}
		delete(m.directoryEntries, directory)
	}
}

// describe answers name's info, and its bytes when it is a file, or a path
// error of operation: fs.ErrInvalid for a name fs.ValidPath rejects,
// fs.ErrNotExist for one not held. The lock is held.
func (m *MemoryFS) describe(operation, name string) (fileInfo, []byte, error) {
	if !fs.ValidPath(name) {
		return fileInfo{}, nil, &fs.PathError{Op: operation, Path: name, Err: fs.ErrInvalid}
	}
	if file, found := m.files[name]; found {
		return fileInfo{name: path.Base(name), size: int64(len(file.data)), mode: 0o444, modTime: file.putAt}, file.data, nil
	}
	if _, found := m.directoryEntries[name]; found {
		return fileInfo{name: path.Base(name), mode: fs.ModeDir | 0o555}, nil, nil
	}
	return fileInfo{}, nil, &fs.PathError{Op: operation, Path: name, Err: fs.ErrNotExist}
}

// listEntries lists the directory name's entries, sorted by name. The lock
// is held.
func (m *MemoryFS) listEntries(name string) []fs.DirEntry {
	names := slices.Sorted(maps.Keys(m.directoryEntries[name]))
	entries := make([]fs.DirEntry, len(names))
	for index, entry := range names {
		info, _, _ := m.describe("readdir", path.Join(name, entry))
		entries[index] = fs.FileInfoToDirEntry(info)
	}
	return entries
}

// stampPut answers the modification time a put now stamps, and keeps it as
// the last: the clock's, or a nanosecond after the last put's when the clock
// has not moved past it. The lock is held.
func (m *MemoryFS) stampPut() time.Time {
	putAt := time.Now().Round(0)
	if !putAt.After(m.lastPutAt) {
		putAt = m.lastPutAt.Add(time.Nanosecond)
	}
	m.lastPutAt = putAt
	return putAt
}

// putFile is a file put into a MemoryFS: its bytes, which nothing changes
// after the put, and when it was put.
type putFile struct {
	data  []byte
	putAt time.Time
}
