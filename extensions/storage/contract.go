// Package storage is the contract of the storage Port: one kernel resource,
// FileSystem, a prioritized read-only overlay over every mounted filesystem,
// including the single permanent one that writes land in. Reading needs a read
// lock on FileSystem; mutating needs a write lock, because write access is
// reachable only through WriteAccess. One resource over one set of bytes is
// what makes the scheduler's read/write locks mean anything here — a reader and
// a writer of the same file conflict instead of racing.
//
// storage is a Port: the plugin, in storageimpl, requires exactly one
// PermanentFS Adapter, which a platform plugin (diskfs, jsfs) provides. storage
// itself carries no platform code: no build tags, no os. Read mounts are plain
// fs.FS values the composition root chooses.
package storage

import (
	"github.com/dvoyni/cog/extensions/storage/internal"
	"github.com/dvoyni/cog/kernel"
)

// Name is the storage plugin name and configuration key.
const Name kernel.PluginName = "storage"

// MountId identifies a filesystem mounted in FileSystem.
type MountId = internal.MountId

// ReadMount is one filesystem in the FileSystem overlay. Higher priorities are
// searched first; equal priorities retain registration order. PermanentMount is
// reserved: it is derived from the permanent filesystem rather than configured,
// so the mount and the write target can never disagree, and it is searched
// before every other mount whatever their priorities.
type ReadMount = internal.ReadMount

// PermanentFS is the Adapter storage requires: the filesystem writes land in.
// An Adapter plugin provides it with registrar.ProvideAdapter[storage.PermanentFS]
// during its Register. storage never hands it back to callers: FileSystem keeps
// it inside and exposes only its readable half, while WriteAccess exposes the
// mutating half to write-lock holders. Operation names use fs.ValidPath form.
type PermanentFS = internal.PermanentFS

const (
	// PermanentMount is the highest-priority read mount, backed by the permanent
	// filesystem so written data reads back through the same overlay. It is
	// reserved: storage derives it, and configuring or mounting it is an error.
	PermanentMount MountId = internal.PermanentMount
	// DefaultReadPriority is the priority of an ordinary read mount.
	DefaultReadPriority = 0
	// DefaultValuesPath is the key-value file used when no values path is
	// configured.
	DefaultValuesPath = "config.json"
)
