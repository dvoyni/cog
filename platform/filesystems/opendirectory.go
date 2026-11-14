package filesystems

import (
	"errors"
	"io"
	"io/fs"
)

// errIsDirectory is why a directory's name cannot be read as a file, put or
// removed.
var errIsDirectory = errors.New("is a directory")

// openDirectory is a directory of an adapter opened for reading: its
// entries as of the open.
type openDirectory struct {
	info    fileInfo
	entries []fs.DirEntry
}

// Stat describes the directory.
func (d *openDirectory) Stat() (fs.FileInfo, error) { return d.info, nil }

// Read fails: a directory has no bytes.
func (d *openDirectory) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errIsDirectory}
}

// ReadDir answers the next count entries, or every remaining one when count
// is not positive, as fs.ReadDirFile does.
func (d *openDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
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
func (d *openDirectory) Close() error { return nil }
