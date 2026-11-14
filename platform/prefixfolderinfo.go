package platform

import (
	"io/fs"
	"time"
)

// prefixFolderInfo describes a folder above a prefix, of name: read-only,
// empty, of no time.
type prefixFolderInfo string

// Name answers the folder's name.
func (i prefixFolderInfo) Name() string { return string(i) }

// Size answers zero.
func (prefixFolderInfo) Size() int64 { return 0 }

// Mode answers a read-only folder.
func (prefixFolderInfo) Mode() fs.FileMode { return fs.ModeDir | 0o555 }

// ModTime answers the zero time.
func (prefixFolderInfo) ModTime() time.Time { return time.Time{} }

// IsDir answers true.
func (prefixFolderInfo) IsDir() bool { return true }

// Sys answers nil.
func (prefixFolderInfo) Sys() any { return nil }
