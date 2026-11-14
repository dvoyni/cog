package platform

import (
	"io/fs"
)

// prefixRoot is the root of a prefixed mount's files, opened as its prefix:
// named for the prefix's last element, not ".".
type prefixRoot struct {
	fs.File
	name string
}

// Stat describes the root under the prefix's name.
func (r prefixRoot) Stat() (fs.FileInfo, error) {
	info, err := r.File.Stat()
	if err != nil {
		return nil, err
	}
	return renamedInfo{FileInfo: info, name: r.name}, nil
}

// ReadDir lists the root, as fs.ReadDirFile does.
func (r prefixRoot) ReadDir(count int) ([]fs.DirEntry, error) {
	directory, ok := r.File.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: r.name, Err: errNotDirectory}
	}
	return directory.ReadDir(count)
}
