package storage

import (
	"io/fs"

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

// GetReadFSCmd invokes the request callback synchronously while holding the
// ReadFS resource lock. The callback must not retain the filesystem or any open
// file after it returns.
type GetReadFSCmd kernel.Command[GetReadFSRequest, GetReadFSResponse]
type GetReadFSRequest struct{ Use func(fs.FS) error }
type GetReadFSResponse struct{}

// ReadFileCmd reads the first matching file from the prioritized ReadFS
// resource and closes it before returning.
type ReadFileCmd kernel.Command[ReadFileRequest, ReadFileResponse]
type ReadFileRequest struct{ Name string }
type ReadFileResponse struct{ Data []byte }

// WriteFileCmd writes a complete file to the permanent WriteFS resource. The
// command takes the WriteFS write lock, serializing it with other mutations.
type WriteFileCmd kernel.Command[WriteFileRequest, WriteFileResponse]
type WriteFileRequest struct {
	Name string
	Data []byte
	Perm fs.FileMode
}
type WriteFileResponse struct{}

// MkdirAllCmd creates a directory and missing parents in the permanent WriteFS
// resource. Existing directories are left unchanged.
type MkdirAllCmd kernel.Command[MkdirAllRequest, MkdirAllResponse]
type MkdirAllRequest struct {
	Path string
	Perm fs.FileMode
}
type MkdirAllResponse struct{}

// RemoveCmd removes one file or empty directory from the permanent WriteFS
// resource.
type RemoveCmd kernel.Command[RemoveRequest, RemoveResponse]
type RemoveRequest struct{ Name string }
type RemoveResponse struct{}

// RenameCmd renames one entry within the permanent WriteFS resource.
type RenameCmd kernel.Command[RenameRequest, RenameResponse]
type RenameRequest struct{ OldName, NewName string }
type RenameResponse struct{}
