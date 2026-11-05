//go:build js

package filesystems

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"syscall/js"
	"time"
)

// LocalStorageWriteFS is a writable filesystem in the page's localStorage,
// for platform's WriteFSSocket: every file in one JSON document under one
// key, read whole by every call and written whole by every change, so what
// one page wrote survives a reload. localStorage keeps a few megabytes per
// origin, so it suits values and saves, not assets. It holds nothing to
// close.
type LocalStorageWriteFS struct {
	key     string
	storage js.Value
}

// NewLocalStorageWriteFS answers the writable filesystem kept under key in
// the page's localStorage, or why there is none: an empty key, or a page
// without localStorage.
func NewLocalStorageWriteFS(key string) (*LocalStorageWriteFS, error) {
	if key == "" {
		return nil, errors.New("filesystems: a localStorage filesystem needs a key")
	}
	storage := js.Global().Get("localStorage")
	if storage.IsUndefined() || storage.IsNull() {
		return nil, errors.New("filesystems: the page has no localStorage")
	}
	return &LocalStorageWriteFS{key: key, storage: storage}, nil
}

// Open opens name for reading: a file, or a directory listing its entries.
func (l *LocalStorageWriteFS) Open(name string) (fs.File, error) {
	document, err := l.load("open", name)
	if err != nil {
		return nil, err
	}
	if file, found := document.Files[name]; found {
		info := localStorageFileInfo{name: path.Base(name), size: int64(len(file.Data)), mode: file.Mode}
		return &localStorageOpenFile{Reader: bytes.NewReader(file.Data), info: info}, nil
	}
	if document.isDirectory(name) {
		info := localStorageFileInfo{name: path.Base(name), mode: document.directoryMode(name)}
		return &localStorageOpenDirectory{info: info, entries: document.entries(name)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// WriteFile writes data to name, creating it with perm or truncating it, in
// a directory that exists.
func (l *LocalStorageWriteFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	document, err := l.load("write", name)
	if err != nil {
		return err
	}
	if err := document.checkFileWritable("write", name); err != nil {
		return err
	}
	document.Files[name] = localStorageFile{Data: slices.Clone(data), Mode: perm.Perm()}
	return l.save("write", name, document)
}

// MkdirAll creates the directory name and any parents it lacks with perm.
func (l *LocalStorageWriteFS) MkdirAll(name string, perm fs.FileMode) error {
	document, err := l.load("mkdir", name)
	if err != nil {
		return err
	}
	for directory := name; directory != "."; directory = path.Dir(directory) {
		if _, found := document.Files[directory]; found {
			return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrExist}
		}
	}
	for directory := name; directory != "." && !document.isDirectory(directory); directory = path.Dir(directory) {
		document.Directories[directory] = perm.Perm()
	}
	return l.save("mkdir", name, document)
}

// Remove removes the file or empty directory name.
func (l *LocalStorageWriteFS) Remove(name string) error {
	document, err := l.load("remove", name)
	if err != nil {
		return err
	}
	if _, found := document.Files[name]; found {
		delete(document.Files, name)
		return l.save("remove", name, document)
	}
	if name == "." || !document.isDirectory(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	if len(document.entries(name)) > 0 {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrExist}
	}
	delete(document.Directories, name)
	return l.save("remove", name, document)
}

// Rename moves oldName, a file or a directory with everything in it, to
// newName in a directory that exists, replacing a file there.
func (l *LocalStorageWriteFS) Rename(oldName, newName string) error {
	document, err := l.load("rename", oldName)
	if err != nil {
		return err
	}
	if !fs.ValidPath(newName) || newName == "." {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrInvalid}
	}
	if file, found := document.Files[oldName]; found {
		if err := document.checkFileWritable("rename", newName); err != nil {
			return err
		}
		delete(document.Files, oldName)
		document.Files[newName] = file
		return l.save("rename", oldName, document)
	}
	if oldName == "." || !document.isDirectory(oldName) {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrNotExist}
	}
	if strings.HasPrefix(newName+"/", oldName+"/") {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrInvalid}
	}
	if _, found := document.Files[newName]; found || document.isDirectory(newName) {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrExist}
	}
	if !document.isDirectory(path.Dir(newName)) {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrNotExist}
	}
	document.move(oldName, newName)
	return l.save("rename", oldName, document)
}

// AppendFile appends data to name, keeping its mode, or creates it with
// perm, in a directory that exists.
func (l *LocalStorageWriteFS) AppendFile(name string, data []byte, perm fs.FileMode) error {
	document, err := l.load("append", name)
	if err != nil {
		return err
	}
	file, found := document.Files[name]
	if !found {
		if err := document.checkFileWritable("append", name); err != nil {
			return err
		}
		file.Mode = perm.Perm()
	}
	file.Data = append(slices.Clone(file.Data), data...)
	document.Files[name] = file
	return l.save("append", name, document)
}

// load answers the document under the key for operation on name, an empty
// one when the key holds none, or why it cannot: an invalid name, or a key
// holding what is not a document.
func (l *LocalStorageWriteFS) load(operation, name string) (localStorageDocument, error) {
	if !fs.ValidPath(name) {
		return localStorageDocument{}, &fs.PathError{Op: operation, Path: name, Err: fs.ErrInvalid}
	}
	document := localStorageDocument{Files: map[string]localStorageFile{}, Directories: map[string]fs.FileMode{}}
	item := l.storage.Call("getItem", l.key)
	if item.IsNull() {
		return document, nil
	}
	if err := json.Unmarshal([]byte(item.String()), &document); err != nil {
		return localStorageDocument{}, &fs.PathError{Op: operation, Path: name, Err: fmt.Errorf("localStorage key %q holds no filesystem: %w", l.key, err)}
	}
	if document.Files == nil {
		document.Files = map[string]localStorageFile{}
	}
	if document.Directories == nil {
		document.Directories = map[string]fs.FileMode{}
	}
	return document, nil
}

// save writes document under the key for operation on name, or answers why
// localStorage refused it, such as its quota.
func (l *LocalStorageWriteFS) save(operation, name string, document localStorageDocument) (err error) {
	data, err := json.Marshal(document)
	if err != nil {
		return &fs.PathError{Op: operation, Path: name, Err: err}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &fs.PathError{Op: operation, Path: name, Err: fmt.Errorf("localStorage refused the write: %v", recovered)}
		}
	}()
	l.storage.Call("setItem", l.key, string(data))
	return nil
}

// localStorageDocument is what a LocalStorageWriteFS keeps under its key:
// every file and every directory, by name. Every directory a name is in is
// one of Directories, the root aside.
type localStorageDocument struct {
	Files       map[string]localStorageFile `json:"files"`
	Directories map[string]fs.FileMode      `json:"directories"`
}

// isDirectory answers whether name is the root or a directory.
func (d localStorageDocument) isDirectory(name string) bool {
	_, found := d.Directories[name]
	return found || name == "."
}

// directoryMode answers the mode of the directory name.
func (d localStorageDocument) directoryMode(name string) fs.FileMode {
	if mode, found := d.Directories[name]; found {
		return fs.ModeDir | mode
	}
	return fs.ModeDir | 0o755
}

// entries lists the directory name's direct children, sorted by name.
func (d localStorageDocument) entries(name string) []fs.DirEntry {
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}
	children := map[string]fs.FileInfo{}
	for file, data := range d.Files {
		if child, found := childOf(file, prefix); found {
			children[child] = localStorageFileInfo{name: child, size: int64(len(data.Data)), mode: data.Mode}
		}
	}
	for directory := range d.Directories {
		if child, found := childOf(directory, prefix); found {
			children[child] = localStorageFileInfo{name: child, mode: d.directoryMode(prefix + child)}
		}
	}
	entries := make([]fs.DirEntry, 0, len(children))
	for _, child := range slices.Sorted(maps.Keys(children)) {
		entries = append(entries, fs.FileInfoToDirEntry(children[child]))
	}
	return entries
}

// checkFileWritable answers why a file cannot be put at name for
// operation, or nil: its directory must exist, and name not be one.
func (d localStorageDocument) checkFileWritable(operation, name string) error {
	if name == "." || d.isDirectory(name) {
		return &fs.PathError{Op: operation, Path: name, Err: fs.ErrExist}
	}
	if !d.isDirectory(path.Dir(name)) {
		return &fs.PathError{Op: operation, Path: name, Err: fs.ErrNotExist}
	}
	return nil
}

// move moves the directory oldName and everything beneath it to newName.
func (d localStorageDocument) move(oldName, newName string) {
	oldPrefix := oldName + "/"
	for file, data := range d.Files {
		if rest, found := strings.CutPrefix(file, oldPrefix); found {
			delete(d.Files, file)
			d.Files[newName+"/"+rest] = data
		}
	}
	moved := map[string]fs.FileMode{}
	for directory, mode := range d.Directories {
		if directory == oldName {
			delete(d.Directories, directory)
			moved[newName] = mode
		} else if rest, found := strings.CutPrefix(directory, oldPrefix); found {
			delete(d.Directories, directory)
			moved[newName+"/"+rest] = mode
		}
	}
	for directory, mode := range moved {
		d.Directories[directory] = mode
	}
}

// localStorageFile is one file of a localStorageDocument.
type localStorageFile struct {
	Data []byte      `json:"data"`
	Mode fs.FileMode `json:"mode"`
}

// localStorageOpenFile is a file of a LocalStorageWriteFS opened for
// reading.
type localStorageOpenFile struct {
	*bytes.Reader
	info localStorageFileInfo
}

// Stat describes the file.
func (f *localStorageOpenFile) Stat() (fs.FileInfo, error) { return f.info, nil }

// Close does nothing.
func (f *localStorageOpenFile) Close() error { return nil }

// localStorageOpenDirectory is a directory of a LocalStorageWriteFS opened
// for reading: its entries as of the open.
type localStorageOpenDirectory struct {
	info    localStorageFileInfo
	entries []fs.DirEntry
}

// Stat describes the directory.
func (d *localStorageOpenDirectory) Stat() (fs.FileInfo, error) { return d.info, nil }

// Read fails: a directory has no bytes.
func (d *localStorageOpenDirectory) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errors.New("is a directory")}
}

// ReadDir answers the next count entries, or every remaining one when count
// is not positive, as fs.ReadDirFile does.
func (d *localStorageOpenDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
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

// Close does nothing.
func (d *localStorageOpenDirectory) Close() error { return nil }

// localStorageFileInfo describes a file or directory of a
// LocalStorageWriteFS, which keeps no times.
type localStorageFileInfo struct {
	name string
	size int64
	mode fs.FileMode
}

// Name answers the base name.
func (i localStorageFileInfo) Name() string { return i.name }

// Size answers the length in bytes, 0 for a directory.
func (i localStorageFileInfo) Size() int64 { return i.size }

// Mode answers the mode.
func (i localStorageFileInfo) Mode() fs.FileMode { return i.mode }

// ModTime answers the zero time.
func (i localStorageFileInfo) ModTime() time.Time { return time.Time{} }

// IsDir answers whether it is a directory.
func (i localStorageFileInfo) IsDir() bool { return i.mode.IsDir() }

// Sys answers nil.
func (i localStorageFileInfo) Sys() any { return nil }

// childOf answers name with prefix cut, when what remains is a direct child.
func childOf(name, prefix string) (string, bool) {
	rest, found := strings.CutPrefix(name, prefix)
	return rest, found && rest != "" && !strings.Contains(rest, "/")
}
