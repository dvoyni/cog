package canvas_test

import (
	"io/fs"
	"sync"
)

// countingFS is files, counting the opens of each path, a read among them,
// and statting its files without opening them.
type countingFS struct {
	files      fs.FS
	mutex      sync.Mutex
	openCounts map[string]int
}

func (c *countingFS) Open(path string) (fs.File, error) {
	c.mutex.Lock()
	c.openCounts[path]++
	c.mutex.Unlock()
	return c.files.Open(path)
}

func (c *countingFS) Stat(path string) (fs.FileInfo, error) {
	return fs.Stat(c.files, path)
}

// countOpens answers how many times path was opened.
func (c *countingFS) countOpens(path string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.openCounts[path]
}
