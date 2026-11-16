package platform

import (
	"errors"
	"io/fs"
	"path"
	"strings"
)

// prefixedFS is a read mount bound with a prefix, as the overlay reads it:
// files's names appear under prefix alone, and each folder above prefix
// appears holding the next one down, so listings and walks reach it.
type prefixedFS struct {
	prefix string
	files  FS
}

// Open opens name: files's name below prefix, or a folder above it.
func (p prefixedFS) Open(name string) (fs.File, error) {
	inner, child, err := p.resolve("open", name)
	switch {
	case err != nil:
		return nil, err
	case child != "":
		return &prefixFolder{name: path.Base(name), child: child}, nil
	case inner == ".":
		file, err := p.files.Open(inner)
		if err != nil {
			return nil, err
		}
		return prefixRoot{File: file, name: path.Base(name)}, nil
	default:
		return p.files.Open(inner)
	}
}

// Stat describes name as Open would open it.
func (p prefixedFS) Stat(name string) (fs.FileInfo, error) {
	inner, child, err := p.resolve("stat", name)
	switch {
	case err != nil:
		return nil, err
	case child != "":
		return prefixFolderInfo(path.Base(name)), nil
	case inner == ".":
		info, err := fs.Stat(p.files, inner)
		if err != nil {
			return nil, err
		}
		return renamedInfo{FileInfo: info, name: path.Base(name)}, nil
	default:
		return fs.Stat(p.files, inner)
	}
}

// ReadDir lists name as Open would open it.
func (p prefixedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	inner, child, err := p.resolve("readdir", name)
	switch {
	case err != nil:
		return nil, err
	case child != "":
		return []fs.DirEntry{fs.FileInfoToDirEntry(prefixFolderInfo(child))}, nil
	default:
		return fs.ReadDir(p.files, inner)
	}
}

// ReadFile reads name, files's name below prefix.
func (p prefixedFS) ReadFile(name string) ([]byte, error) {
	inner, child, err := p.resolve("readfile", name)
	switch {
	case err != nil:
		return nil, err
	case child != "":
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: errNotFile}
	default:
		return fs.ReadFile(p.files, inner)
	}
}

// resolve answers files's name for name, a valid path, when name is prefix
// or below it, or the folder name holds on the way down to prefix when it
// is above it; fs.ErrNotExist, under op, for any other name.
func (p prefixedFS) resolve(op, name string) (string, string, error) {
	switch {
	case name == p.prefix:
		return ".", "", nil
	case strings.HasPrefix(name, p.prefix+"/"):
		return name[len(p.prefix)+1:], "", nil
	case name == ".":
		child, _, _ := strings.Cut(p.prefix, "/")
		return "", child, nil
	case strings.HasPrefix(p.prefix, name+"/"):
		child, _, _ := strings.Cut(p.prefix[len(name)+1:], "/")
		return "", child, nil
	default:
		return "", "", &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
}

// errNotFile is why reading a name that is a folder fails.
var errNotFile = errors.New("is a directory")
