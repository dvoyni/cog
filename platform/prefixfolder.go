package platform

import (
	"io"
	"io/fs"
)

// prefixFolder is an opened folder above a prefix, holding child alone.
type prefixFolder struct {
	name, child string
	listed      bool
}

// Stat describes the folder.
func (f *prefixFolder) Stat() (fs.FileInfo, error) { return prefixFolderInfo(f.name), nil }

// Read fails: the folder is no file.
func (f *prefixFolder) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: f.name, Err: errNotFile}
}

// Close does nothing.
func (*prefixFolder) Close() error { return nil }

// ReadDir answers child's entry once, then nothing, as fs.ReadDirFile does.
func (f *prefixFolder) ReadDir(count int) ([]fs.DirEntry, error) {
	if f.listed {
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	f.listed = true
	return []fs.DirEntry{fs.FileInfoToDirEntry(prefixFolderInfo(f.child))}, nil
}
