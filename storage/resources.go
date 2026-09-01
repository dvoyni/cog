package storage

import (
	"io/fs"
)

// ReadFS is the immutable prioritized overlay used for reading. It covers every
// readable filesystem, including the permanent one, so a caller never chooses a
// filesystem to read from. Access it only while a handler holds its declared
// resource lock; do not retain the resource or files opened from it after the
// handler returns.
type ReadFS = readFS

// NewReadFS builds a ReadFS backed by a single mounted filesystem. The plugin
// wires ReadFS from configured mounts at runtime; this constructor is for tests
// and embedders that need a standalone ReadFS over an fs.FS.
func NewReadFS(id MountId, filesystem fs.FS) ReadFS {
	return newReadFS([]ReadMount{{Id: id, Priority: DefaultReadPriority, FS: filesystem}})
}

// WriteFS is the permanent filesystem resource, and the only writable one. The
// plugin also mounts it into ReadFS, so reads go through that overlay rather
// than this resource. Access it only while a handler holds its declared
// resource lock, using write access for mutations; do not retain the resource
// after the handler returns.
type WriteFS = writeFS
