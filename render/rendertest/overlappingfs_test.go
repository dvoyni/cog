package rendertest_test

import (
	"io/fs"
	"sync/atomic"
	"time"
)

// overlappingFS is files whose first two opens of name each wait, up to a
// second, for the other to begin, so two first acquires of its texture read
// its header at once.
type overlappingFS struct {
	fs.FS
	name        string
	openedCount atomic.Int32
}

// Open opens name in the files, waiting first for the other of the first two
// opens of f.name to begin.
func (f *overlappingFS) Open(name string) (fs.File, error) {
	if name == f.name && f.openedCount.Add(1) <= 2 {
		for deadline := time.Now().Add(time.Second); f.openedCount.Load() < 2 && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		}
	}
	return f.FS.Open(name)
}
