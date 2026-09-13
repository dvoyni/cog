// Package storage provides one kernel resource, FileSystem: a prioritized
// read-only overlay over every mounted filesystem, including the single
// permanent one that writes land in. Reading needs a read lock on FileSystem;
// mutating needs a write lock, because write access is reachable only through
// WriteAccess. One resource over one set of bytes is what makes the scheduler's
// read/write locks mean anything here — a reader and a writer of the same file
// now conflict instead of racing.
package storage

import (
	"io/fs"
)

// MountId identifies a filesystem mounted in FileSystem.
type MountId string

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

// PermanentFS is what a permanent filesystem backend implements. Backends are
// handed to storage through Config or SetPermanentFSCmd and are never returned
// to callers: FileSystem keeps one inside and exposes only its readable half,
// while WriteAccess exposes the mutating half to write-lock holders.
type PermanentFS interface {
	fs.FS
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(name string) error
	Rename(oldName, newName string) error
}
