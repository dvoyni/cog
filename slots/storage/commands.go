package storage

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage/internal/types"
)

// SetMountCmd adds or replaces one mount in the FileSystem overlay. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
// PermanentMount is reserved to the permanent filesystem.
type SetMountCmd kernel.Command[SetMountRequest, SetMountResponse]
type SetMountRequest struct{ Mount ReadMount }

// SetMountResponse reports a mount the command refused. A mount with no id or
// no filesystem, and one naming PermanentMount, are outcomes the caller asked
// for and can act on, so they answer here rather than being reported.
type SetMountResponse struct{ Err error }

// RemoveMountCmd removes one mount from the FileSystem overlay by id.
// PermanentMount cannot be removed: it is derived from the permanent
// filesystem, and dropping it would leave written data unreadable.
type RemoveMountCmd kernel.Command[RemoveMountRequest, RemoveMountResponse]
type RemoveMountRequest struct{ Id MountId }

// RemoveMountResponse reports whether a mount was there to remove, and the
// refusal when the id was PermanentMount.
type RemoveMountResponse struct {
	Removed bool
	Err     error
}

// AccessValuesCmd is the single entry point to the key-value store. One
// command covers reads, writes, deletions and flushes because they are one
// operation from the store's side: each may load the values file, change the
// cache, and write it back, all under the same pair of locks. Build its request
// with GetValue, SetValue, DeleteValue or FlushValues.
type AccessValuesCmd kernel.Command[AccessValuesRequest, AccessValuesResponse]

// AccessValuesRequest is one operation on the key-value store. Its zero value
// names no operation.
type AccessValuesRequest = types.AccessValuesRequest

// AccessValuesResponse reports whether the operation found its key, and the
// failure when one stopped it: an invalid request, a values file that would
// not load, or a flush that would not write.
type AccessValuesResponse = types.AccessValuesResponse
