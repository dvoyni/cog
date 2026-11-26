package font_test

import "io/fs"

// countingFS is files, counting the opens of each path.
type countingFS struct {
	files      fs.FS
	openCounts map[string]int
}

func (c *countingFS) Open(path string) (fs.File, error) {
	c.openCounts[path]++
	return c.files.Open(path)
}
