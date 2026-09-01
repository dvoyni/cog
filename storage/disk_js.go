//go:build js

package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"syscall/js"
	"time"
)

const webStoragePrefix = "cog.storage."

func defaultReadMount() (ReadMount, bool, error) {
	return ReadMount{}, false, nil
}

func defaultWriteFS(appId string) (WriteFS, error) {
	localStorage := js.Global().Get("localStorage")
	if localStorage.IsUndefined() || localStorage.IsNull() {
		return nil, errors.New("storage: browser localStorage is unavailable")
	}
	return &webFS{key: webStoragePrefix + appId, storage: localStorage}, nil
}

// OpenDiskFS is unavailable in a browser; use the default localStorage backend
// or provide a WriteFS through Config.WithWriteFS.
func OpenDiskFS(path string) (WriteFS, error) {
	return nil, fmt.Errorf("storage: disk filesystem %q is unavailable in a browser", path)
}

type webFS struct {
	key     string
	storage js.Value
}

type webFSState struct {
	Files map[string]webFile `json:"files"`
	Dirs  map[string]uint32  `json:"dirs"`
}

type webFile struct {
	Data []byte `json:"data"`
	Mode uint32 `json:"mode"`
}

func (w *webFS) Open(name string) (fs.File, error) {
	if err := validatePath("open", name); err != nil {
		return nil, err
	}
	state, err := w.load()
	if err != nil {
		return nil, err
	}
	if file, ok := state.Files[name]; ok {
		return &webOpenFile{
			Reader: bytes.NewReader(file.Data),
			info:   webFileInfo{name: path.Base(name), size: int64(len(file.Data)), mode: fs.FileMode(file.Mode)},
		}, nil
	}
	if name == "." || state.hasDir(name) {
		return &webOpenDir{info: webFileInfo{name: path.Base(name), mode: fs.ModeDir | 0o700}, entries: state.entries(name)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (w *webFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := validatePath("write", name); err != nil {
		return err
	}
	state, err := w.load()
	if err != nil {
		return err
	}
	if parent := path.Dir(name); parent != "." && !state.hasDir(parent) {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrNotExist}
	}
	state.Files[name] = webFile{Data: append([]byte(nil), data...), Mode: uint32(perm)}
	return w.save(state)
}

func (w *webFS) MkdirAll(name string, perm fs.FileMode) error {
	if err := validatePath("mkdir", name); err != nil {
		return err
	}
	state, err := w.load()
	if err != nil {
		return err
	}
	for current := name; current != "."; current = path.Dir(current) {
		state.Dirs[current] = uint32(perm)
	}
	return w.save(state)
}

func (w *webFS) Remove(name string) error {
	if err := validatePath("remove", name); err != nil {
		return err
	}
	state, err := w.load()
	if err != nil {
		return err
	}
	if _, ok := state.Files[name]; ok {
		delete(state.Files, name)
		return w.save(state)
	}
	if !state.hasDir(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	prefix := name + "/"
	for entry := range state.Files {
		if strings.HasPrefix(entry, prefix) {
			return &fs.PathError{Op: "remove", Path: name, Err: errors.New("directory not empty")}
		}
	}
	for entry := range state.Dirs {
		if entry != name && strings.HasPrefix(entry, prefix) {
			return &fs.PathError{Op: "remove", Path: name, Err: errors.New("directory not empty")}
		}
	}
	delete(state.Dirs, name)
	return w.save(state)
}

func (w *webFS) Rename(oldName, newName string) error {
	if err := validatePath("rename", oldName); err != nil {
		return err
	}
	if err := validatePath("rename", newName); err != nil {
		return err
	}
	state, err := w.load()
	if err != nil {
		return err
	}
	if file, ok := state.Files[oldName]; ok {
		delete(state.Files, oldName)
		state.Files[newName] = file
		return w.save(state)
	}
	if !state.hasDir(oldName) {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrNotExist}
	}
	if strings.HasPrefix(newName+"/", oldName+"/") {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrInvalid}
	}
	w.renameTree(state, oldName, newName)
	return w.save(state)
}

func (w *webFS) renameTree(state webFSState, oldName, newName string) {
	oldPrefix := oldName + "/"
	for name, file := range state.Files {
		if strings.HasPrefix(name, oldPrefix) {
			delete(state.Files, name)
			state.Files[newName+strings.TrimPrefix(name, oldName)] = file
		}
	}
	for name, mode := range state.Dirs {
		if name == oldName || strings.HasPrefix(name, oldPrefix) {
			delete(state.Dirs, name)
			state.Dirs[newName+strings.TrimPrefix(name, oldName)] = mode
		}
	}
}

func (w *webFS) load() (webFSState, error) {
	state := webFSState{Files: map[string]webFile{}, Dirs: map[string]uint32{}}
	raw := w.storage.Call("getItem", w.key)
	if raw.IsNull() {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw.String()), &state); err != nil {
		return webFSState{}, fmt.Errorf("storage: decode browser data: %w", err)
	}
	if state.Files == nil {
		state.Files = map[string]webFile{}
	}
	if state.Dirs == nil {
		state.Dirs = map[string]uint32{}
	}
	return state, nil
}

func (w *webFS) save(state webFSState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("storage: encode browser data: %w", err)
	}
	w.storage.Call("setItem", w.key, string(data))
	return nil
}

func (s webFSState) hasDir(name string) bool {
	if _, ok := s.Dirs[name]; ok {
		return true
	}
	prefix := name + "/"
	for entry := range s.Files {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	for entry := range s.Dirs {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func (s webFSState) entries(dir string) []fs.DirEntry {
	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}
	entries := map[string]webFileInfo{}
	for name, file := range s.Files {
		if rest, ok := directChild(name, prefix); ok {
			entries[rest] = webFileInfo{name: rest, size: int64(len(file.Data)), mode: fs.FileMode(file.Mode)}
		}
	}
	for name, mode := range s.Dirs {
		if rest, ok := directChild(name, prefix); ok {
			entries[rest] = webFileInfo{name: rest, mode: fs.ModeDir | fs.FileMode(mode)}
		}
	}
	result := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, webDirEntry{info: entry})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

func directChild(name, prefix string) (string, bool) {
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	return rest, rest != "" && !strings.Contains(rest, "/")
}

type webOpenFile struct {
	*bytes.Reader
	info webFileInfo
}

func (f *webOpenFile) Close() error               { return nil }
func (f *webOpenFile) Stat() (fs.FileInfo, error) { return f.info, nil }

type webOpenDir struct {
	info    webFileInfo
	entries []fs.DirEntry
	offset  int
}

func (d *webOpenDir) Close() error               { return nil }
func (d *webOpenDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *webOpenDir) Read([]byte) (int, error)   { return 0, errors.New("is a directory") }
func (d *webOpenDir) ReadDir(count int) ([]fs.DirEntry, error) {
	if d.offset >= len(d.entries) && count > 0 {
		return nil, io.EOF
	}
	end := len(d.entries)
	if count > 0 && d.offset+count < end {
		end = d.offset + count
	}
	entries := d.entries[d.offset:end]
	d.offset = end
	return entries, nil
}

type webFileInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (i webFileInfo) Name() string       { return i.name }
func (i webFileInfo) Size() int64        { return i.size }
func (i webFileInfo) Mode() fs.FileMode  { return i.mode }
func (i webFileInfo) ModTime() time.Time { return time.Time{} }
func (i webFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i webFileInfo) Sys() any           { return nil }

type webDirEntry struct{ info webFileInfo }

func (e webDirEntry) Name() string               { return e.info.Name() }
func (e webDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e webDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e webDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

var _ WriteFS = (*webFS)(nil)
var _ fs.ReadDirFile = (*webOpenDir)(nil)
