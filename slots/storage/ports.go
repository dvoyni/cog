package storage

import (
	"github.com/dvoyni/cog/slots/storage/internal"
)

// PermanentFS is the interface storage's Adapter implements: the filesystem
// writes land in. An Extension fills PermanentFSPort with
// registrar.ProvideAdapter during its Register. storage never hands it back to
// callers: FileSystem keeps it inside and exposes only its readable half, while
// WriteAccess exposes the mutating half to write-lock holders. Operation names
// use fs.ValidPath form.
//
// It is declared in internal, beside the FileSystem that holds it, and
// aliased here:
//
//	interface {
//		fs.FS
//		WriteFile(name string, data []byte, perm fs.FileMode) error
//		MkdirAll(path string, perm fs.FileMode) error
//		Remove(name string) error
//		Rename(oldName, newName string) error
//	}
type PermanentFS = internal.PermanentFS

// PermanentFSPort is the Port storage requires exactly one Adapter for: the
// permanent filesystem an Extension such as diskstorage or jsstorage provides.
// A composition without one fails with kernel.ErrMissingAdapter.
type PermanentFSPort = internal.PermanentFSPort

// ReadMountPort is the Port storage collects every read mount through, zero
// included. It is built on ReadMount itself, since a mount is plain data: any
// plugin, a game's own included, declares an Adapter for it and contributes
// its mounts during Register, one ProvideAdapter call per mount:
//
//	type StorageReadMount kernel.Adapter[storage.ReadMountPort]
//
//	registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
//		Id: "res", Priority: storage.DefaultReadPriority, FS: res,
//	})
//
// storage installs them at its Start, so they are in FileSystem before any
// plugin that depends on storage starts. A mount with an empty id or a nil FS
// fails that Start with ErrInvalidMount, PermanentMount with ErrReservedMount,
// and an id contributed twice with ErrDuplicateMount.
type ReadMountPort = internal.ReadMountPort
