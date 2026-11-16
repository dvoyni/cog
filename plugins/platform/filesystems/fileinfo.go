package filesystems

import (
	"io/fs"
	"time"
)

// fileInfo describes a file or directory of an adapter that keeps its files
// itself, its modification time the zero time where the adapter keeps none.
type fileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

// Name answers the base name.
func (i fileInfo) Name() string { return i.name }

// Size answers the length in bytes, 0 for a directory.
func (i fileInfo) Size() int64 { return i.size }

// Mode answers the mode.
func (i fileInfo) Mode() fs.FileMode { return i.mode }

// ModTime answers the modification time.
func (i fileInfo) ModTime() time.Time { return i.modTime }

// IsDir answers whether it is a directory.
func (i fileInfo) IsDir() bool { return i.mode.IsDir() }

// Sys answers nil.
func (i fileInfo) Sys() any { return nil }
