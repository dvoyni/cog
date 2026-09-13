package internal

import "io/fs"

// MountId identifies a filesystem mounted in FileSystem.
type MountId string

// PermanentMount is the highest-priority read mount, backed by the permanent
// filesystem so written data reads back through the same overlay. It is
// reserved: storage derives it, and configuring or mounting it is an error.
const PermanentMount MountId = "permanent"

// ReadMount is one filesystem in the FileSystem overlay. Higher priorities are
// searched first; equal priorities retain registration order. PermanentMount is
// reserved: it is derived from the permanent filesystem rather than configured,
// so the mount and the write target can never disagree, and it is searched
// before every other mount whatever their priorities.
type ReadMount struct {
	Id       MountId
	Priority int
	FS       fs.FS
}

// PermanentFS is the Adapter storage requires: the filesystem writes land in.
// An Adapter plugin provides it with ProvideAdapter[storage.PermanentFS], and
// storage never hands it back to callers: FileSystem keeps it inside and
// exposes only its readable half, while WriteAccess exposes the mutating half
// to write-lock holders.
type PermanentFS interface {
	fs.FS
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(name string) error
	Rename(oldName, newName string) error
}
