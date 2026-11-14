package filesystems

import (
	"bytes"
	"io/fs"
)

// openFile is a file of an adapter opened for reading: the bytes it held at
// the open, which nothing changes after.
type openFile struct {
	*bytes.Reader
	info fileInfo
}

// Stat describes the file.
func (f *openFile) Stat() (fs.FileInfo, error) { return f.info, nil }

// Close does nothing.
func (f *openFile) Close() error { return nil }
