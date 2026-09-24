package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// WriteFS is the mutating half of the permanent filesystem, reachable only
// through WriteAccess. A zero value has no backend and reports ErrNoWriteAccess
// instead of panicking, so a handler holding an unbound write handle fails
// loudly rather than silently writing nowhere.
type WriteFS struct{ permanent PermanentFS }

// WriteAccess returns the mutating half of the permanent filesystem behind a
// write lock on FileSystem.
func WriteAccess(handle kernel.Write[FileSystem]) WriteFS {
	return writeAccess(handle.Get())
}

// writeAccess returns the mutating half of f's permanent filesystem. The
// caller is responsible for holding the write lock f came from.
func writeAccess(f FileSystem) WriteFS {
	if f.permanent == nil {
		return WriteFS{}
	}
	return WriteFS{permanent: f.permanent()}
}

func (w WriteFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "write", Path: name}
	}
	return w.permanent.WriteFile(name, data, perm)
}

func (w WriteFS) MkdirAll(name string, perm fs.FileMode) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "mkdir", Path: name}
	}
	return w.permanent.MkdirAll(name, perm)
}

func (w WriteFS) Remove(name string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "remove", Path: name}
	}
	return w.permanent.Remove(name)
}

func (w WriteFS) Rename(oldName, newName string) error {
	if w.permanent == nil {
		return ErrNoWriteAccess{Op: "rename", Path: oldName}
	}
	return w.permanent.Rename(oldName, newName)
}
