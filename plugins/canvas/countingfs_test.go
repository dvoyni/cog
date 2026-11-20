package canvas_test

import (
	"io/fs"
	"sync"
)

// countingFS is files, counting the opens of each path.
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

// countOpens answers how many times path was opened.
func (c *countingFS) countOpens(path string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.openCounts[path]
}
