package storage

import (
	"github.com/dvoyni/cog/kernel"
)

// SetReadFSCmd adds or replaces one mount in the ReadFS resource. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
type SetReadFSCmd kernel.Command[SetReadFSRequest, SetReadFSResponse]
type SetReadFSRequest struct{ Mount ReadMount }
type SetReadFSResponse struct{}

// RemoveReadFSCmd removes one mount from the ReadFS resource by id.
type RemoveReadFSCmd kernel.Command[RemoveReadFSRequest, RemoveReadFSResponse]
type RemoveReadFSRequest struct{ Id MountId }
type RemoveReadFSResponse struct{ Removed bool }

// SetWriteFSCmd replaces the permanent WriteFS resource.
type SetWriteFSCmd kernel.Command[SetWriteFSRequest, SetWriteFSResponse]
type SetWriteFSRequest struct{ FS WriteFS }
type SetWriteFSResponse struct{}
