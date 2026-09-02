package storage

import (
	"github.com/dvoyni/cog/kernel"
)

// SetMountCmd adds or replaces one mount in the FileSystem overlay. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
// PermanentMount is reserved to SetPermanentFSCmd.
type SetMountCmd kernel.Command[SetMountRequest, SetMountResponse]
type SetMountRequest struct{ Mount ReadMount }
type SetMountResponse struct{}

// RemoveMountCmd removes one mount from the FileSystem overlay by id.
// PermanentMount cannot be removed: it is derived from the permanent
// filesystem, and dropping it would leave written data unreadable.
type RemoveMountCmd kernel.Command[RemoveMountRequest, RemoveMountResponse]
type RemoveMountRequest struct{ Id MountId }
type RemoveMountResponse struct{ Removed bool }

// SetPermanentFSCmd replaces the permanent filesystem. It swaps the write
// target and the PermanentMount overlay entry in one step, so reads never fall
// through to the filesystem that was just replaced.
type SetPermanentFSCmd kernel.Command[SetPermanentFSRequest, SetPermanentFSResponse]
type SetPermanentFSRequest struct{ FS PermanentFS }
type SetPermanentFSResponse struct{}

// AccessValuesCmd is the single entry point to the key-value store. One
// command covers reads, writes, deletions and flushes because they are one
// operation from the store's side: each may load the values file, change the
// cache, and write it back, all under the same pair of locks. Build its request
// with GetValue, SetValue, DeleteValue or FlushValues.
type AccessValuesCmd kernel.Command[AccessValuesRequest, AccessValuesResponse]
type AccessValuesRequest struct{ op valueOp }
type AccessValuesResponse struct{ Found bool }
