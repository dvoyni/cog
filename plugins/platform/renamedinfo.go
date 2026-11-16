package platform

import (
	"io/fs"
)

// renamedInfo describes a file as its FileInfo does, under name.
type renamedInfo struct {
	fs.FileInfo
	name string
}

// Name answers name.
func (i renamedInfo) Name() string { return i.name }
