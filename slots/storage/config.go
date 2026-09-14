package storage

import (
	"io/fs"
	"slices"
)

// DefaultValuesPath is the key-value file used when Config names none.
const DefaultValuesPath = "config.json"

// Config configures storage's read mounts and values file. It is supplied under
// Name, and its zero value is the default: no read mounts and
// DefaultValuesPath. What persists is not configured here: it is the
// PermanentFS Adapter an Extension provides.
type Config struct {
	// ReadMounts are the filesystems the overlay reads, besides the permanent
	// one. Each is a plain fs.FS the composition root chooses: an embedded FS, a
	// preloaded bundle, or os.DirFS on a desktop.
	ReadMounts []ReadMount
	// ValuesPath is the JSON object read through FileSystem and flushed to the
	// permanent filesystem by the value commands. Empty means
	// DefaultValuesPath.
	ValuesPath string
}

// WithReadFS adds or replaces a read mount. The returned Config owns its mount
// slice and does not mutate c. PermanentMount is reserved; the plugin rejects a
// Config that mounts it.
func (c Config) WithReadFS(id MountId, priority int, filesystem fs.FS) Config {
	c.ReadMounts = slices.Clone(c.ReadMounts)
	for i := range c.ReadMounts {
		if c.ReadMounts[i].Id == id {
			c.ReadMounts[i] = ReadMount{Id: id, Priority: priority, FS: filesystem}
			return c
		}
	}
	c.ReadMounts = append(c.ReadMounts, ReadMount{Id: id, Priority: priority, FS: filesystem})
	return c
}

// WithValuesPath replaces the file backing the value commands.
func (c Config) WithValuesPath(path string) Config {
	c.ValuesPath = path
	return c
}
