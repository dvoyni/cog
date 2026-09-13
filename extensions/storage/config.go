package storage

import (
	"io/fs"
	"os"
	"slices"
)

const (
	// ExecutableMount is the default lowest-priority read mount.
	ExecutableMount MountId = "executable"
	// PermanentMount is the highest-priority read mount, backed by the permanent
	// filesystem so written data reads back through the same overlay. It is
	// reserved: storage derives it, and configuring or mounting it is an error.
	PermanentMount MountId = "permanent"
	// DefaultReadPriority is used by WithReadDiskFS.
	DefaultReadPriority = 0
	// DefaultValuesPath is the key-value file used when Config.ValuesPath is empty.
	DefaultValuesPath = "config.json"
)

// Config configures storage's read mounts and permanent filesystem.
type Config struct {
	AppId       string
	ReadMounts  []ReadMount
	PermanentFS PermanentFS
	// ValuesPath is the JSON object read through FileSystem and flushed to the
	// permanent filesystem by the value commands. Empty means DefaultValuesPath.
	ValuesPath string
}

// DefaultConfig returns storage configuration for appId. An empty appId uses
// the executable name when the plugin resolves platform defaults.
func DefaultConfig(appId string) Config {
	return Config{AppId: appId}
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

// WithReadDiskFS mounts path through os.DirFS. The path itself is also the
// mount id, and the mount uses DefaultReadPriority.
func (c Config) WithReadDiskFS(path MountId) Config {
	return c.WithReadFS(path, DefaultReadPriority, os.DirFS(string(path)))
}

// WithPermanentFS replaces the permanent filesystem.
func (c Config) WithPermanentFS(filesystem PermanentFS) Config {
	c.PermanentFS = filesystem
	return c
}

// WithValuesPath replaces the file backing the value commands.
func (c Config) WithValuesPath(path string) Config {
	c.ValuesPath = path
	return c
}
