package storageimpl

import (
	"fmt"
	"io/fs"
	"slices"

	"github.com/dvoyni/cog/extensions/storage"
)

// Config configures storage's read mounts and values file. It is supplied under
// storage.Name. What persists is not configured here: it is the PermanentFS
// Adapter a platform plugin provides.
type Config struct {
	// ReadMounts are the filesystems the overlay reads, besides the permanent
	// one. Each is a plain fs.FS the composition root chooses: an embedded FS, a
	// preloaded bundle, or os.DirFS on a desktop.
	ReadMounts []storage.ReadMount
	// ValuesPath is the JSON object read through FileSystem and flushed to the
	// permanent filesystem by the value commands. Empty means
	// storage.DefaultValuesPath.
	ValuesPath string
}

// DefaultConfig returns storage configuration with no read mounts and the
// default values file.
func DefaultConfig() Config {
	return Config{}
}

// WithReadFS adds or replaces a read mount. The returned Config owns its mount
// slice and does not mutate c. storage.PermanentMount is reserved; the plugin
// rejects a Config that mounts it.
func (c Config) WithReadFS(id storage.MountId, priority int, filesystem fs.FS) Config {
	c.ReadMounts = slices.Clone(c.ReadMounts)
	for i := range c.ReadMounts {
		if c.ReadMounts[i].Id == id {
			c.ReadMounts[i] = storage.ReadMount{Id: id, Priority: priority, FS: filesystem}
			return c
		}
	}
	c.ReadMounts = append(c.ReadMounts, storage.ReadMount{Id: id, Priority: priority, FS: filesystem})
	return c
}

// WithValuesPath replaces the file backing the value commands.
func (c Config) WithValuesPath(path string) Config {
	c.ValuesPath = path
	return c
}

// ErrInvalidConfig reports a non-Config plugin configuration value.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("storage: invalid config: want %T, got %T", Config{}, e.Got)
}
