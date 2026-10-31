package storage

import "github.com/dvoyni/cog/slots/storage/internal"

// MountId identifies a filesystem mounted in FileSystem.
type MountId = internal.MountId

// ReadMount is one filesystem in the FileSystem overlay. Higher priorities are
// searched first; equal priorities retain registration order. PermanentMount is
// reserved: it is derived from the permanent filesystem rather than configured,
// so the mount and the write target can never disagree, and it is searched
// before every other mount whatever their priorities.
type ReadMount = internal.ReadMount

const (
	// PermanentMount is the highest-priority read mount, backed by the permanent
	// filesystem so written data reads back through the same overlay. It is
	// reserved: storage derives it, and configuring or mounting it is an error.
	PermanentMount = internal.PermanentMount
	// DefaultReadPriority is the priority of an ordinary read mount.
	DefaultReadPriority = internal.DefaultReadPriority
)

// WriteFS is write access to the permanent filesystem. Only WriteAccess
// produces one, so by the time any method here runs the scheduler has already
// granted an exclusive lock on FileSystem: no reader can be looking at a file
// mid-write. Do not retain it after the handler returns.
type WriteFS = internal.WriteFS
