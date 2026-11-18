// Package filesystems stands in for platform's memory mount.
package filesystems

import (
	"bytes"
	"io/fs"
	"sync"
	"testing/fstest"
)

// MemoryFS is a memory mount: a read mount held in memory, whose owner puts
// files in and removes them at run time. Every plugin reads its files through
// platform's FS.
type MemoryFS struct {
	lock  sync.RWMutex
	files fstest.MapFS
}

// NewMemoryFS answers an empty memory mount.
func NewMemoryFS() *MemoryFS { return &MemoryFS{files: fstest.MapFS{}} }

// Put puts a copy of data in as the file name, replacing the file there.
func (m *MemoryFS) Put(name string, data []byte) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.files[name] = &fstest.MapFile{Data: bytes.Clone(data)}
	return nil
}

// Open opens the file name.
func (m *MemoryFS) Open(name string) (fs.File, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.files.Open(name)
}
