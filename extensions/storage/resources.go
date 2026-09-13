package storage

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// FileSystem is the single storage resource. It reads through a prioritized
// overlay covering every mounted filesystem, including the permanent one, so a
// caller never chooses a filesystem to read from. It exposes no mutators at
// all: changing files means holding a write lock and passing the handle to
// WriteAccess. Access it only while a handler holds its declared resource lock;
// do not retain the resource or files opened from it after the handler returns.
type FileSystem = fileSystem

// NewFileSystem builds a FileSystem over a single mounted filesystem, with no
// permanent filesystem behind it. The plugin wires the real resource from
// configured mounts at runtime; this constructor is for tests and embedders
// that need a standalone reader over an fs.FS.
func NewFileSystem(id MountId, filesystem fs.FS) FileSystem {
	return newFileSystem([]ReadMount{{Id: id, Priority: DefaultReadPriority, FS: filesystem}}, nil)
}

// WriteFS is write access to the permanent filesystem. Only WriteAccess
// produces one, so by the time any method here runs the scheduler has already
// granted an exclusive lock on FileSystem: no reader can be looking at a file
// mid-write. Do not retain it after the handler returns.
type WriteFS = writeFS

// WriteAccess turns a write lock on FileSystem into write access. Demanding the
// kernel.Write handle is the entire mechanism: a read lock cannot produce one,
// so a mutation under a shared lock does not compile. Reads stay on the
// FileSystem value itself, which the same handle also yields through Get.
func WriteAccess(handle kernel.Write[FileSystem]) WriteFS {
	return writeFS{permanent: handle.Get().permanent}
}

// Values is the key-value store backing the value commands. It caches the
// values file after the first read, so every later operation is in memory until
// a flush. Access it only while a handler holds its declared resource lock;
// because a read also populates the cache, handlers declare write access.
type Values = valueStore
