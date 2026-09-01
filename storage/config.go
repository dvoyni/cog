package storage

import (
	"io/fs"
	"os"
	"slices"
)

const (
	// ExecutableMount is the default lowest-priority read mount.
	ExecutableMount MountId = "executable"
	// PermanentMount is the default highest-priority read mount, backed by the
	// permanent WriteFS so written data reads back through the same overlay.
	PermanentMount MountId = "permanent"
	// DefaultReadPriority is used by WithReadDiskFS.
	DefaultReadPriority = 0
)

// Config configures storage's read mounts and permanent filesystem.
type Config struct {
	AppId      string
	ReadMounts []ReadMount
	WriteFS    WriteFS
}

// DefaultConfig returns storage configuration for appId. An empty appId uses
// the executable name when the plugin resolves platform defaults.
func DefaultConfig(appId string) Config {
	return Config{AppId: appId}
}

// WithReadFS adds or replaces a read mount. The returned Config owns its mount
// slice and does not mutate c.
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

// WithWriteFS replaces the permanent filesystem.
func (c Config) WithWriteFS(filesystem WriteFS) Config {
	c.WriteFS = filesystem
	return c
}
