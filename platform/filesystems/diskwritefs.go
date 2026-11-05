package filesystems

import (
	"io/fs"
	"os"
	"path/filepath"
)

// DiskWriteFS is a writable filesystem in a directory on disk, for
// platform's WriteFSSocket: every name stays inside the directory, symbolic
// links included. It keeps the directory open until Close, which the plugin
// that binds it calls in its Destroy.
type DiskWriteFS struct {
	root *os.Root
	// files is root as an fs.FS, which reads.
	files fs.FS
}

// NewDiskWriteFS opens directory as a writable filesystem, creating it with
// its parents when missing, or answers why it cannot. A relative directory
// resolves against the working directory, here and once.
func NewDiskWriteFS(directory string) (*DiskWriteFS, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &DiskWriteFS{root: root, files: root.FS()}, nil
}

// Open opens name for reading.
func (d *DiskWriteFS) Open(name string) (fs.File, error) { return d.files.Open(name) }

// ReadFile reads name whole.
func (d *DiskWriteFS) ReadFile(name string) ([]byte, error) { return fs.ReadFile(d.files, name) }

// ReadDir lists the directory name, sorted by name.
func (d *DiskWriteFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(d.files, name) }

// Stat describes name.
func (d *DiskWriteFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(d.files, name) }

// WriteFile writes data to name, creating it with perm or truncating it.
func (d *DiskWriteFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	local, err := localize("write", name)
	if err != nil {
		return err
	}
	return d.root.WriteFile(local, data, perm)
}

// MkdirAll creates the directory path and any parents it lacks with perm.
func (d *DiskWriteFS) MkdirAll(path string, perm fs.FileMode) error {
	local, err := localize("mkdir", path)
	if err != nil {
		return err
	}
	return d.root.MkdirAll(local, perm)
}

// Remove removes the file or empty directory name.
func (d *DiskWriteFS) Remove(name string) error {
	local, err := localize("remove", name)
	if err != nil {
		return err
	}
	return d.root.Remove(local)
}

// Rename moves oldName to newName, replacing a file there.
func (d *DiskWriteFS) Rename(oldName, newName string) error {
	oldLocal, err := localize("rename", oldName)
	if err != nil {
		return err
	}
	newLocal, err := localize("rename", newName)
	if err != nil {
		return err
	}
	return d.root.Rename(oldLocal, newLocal)
}

// AppendFile appends data to name, creating it with perm.
func (d *DiskWriteFS) AppendFile(name string, data []byte, perm fs.FileMode) error {
	local, err := localize("append", name)
	if err != nil {
		return err
	}
	file, err := d.root.OpenFile(local, os.O_WRONLY|os.O_APPEND|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Close closes the directory; every call after it fails.
func (d *DiskWriteFS) Close() error { return d.root.Close() }

// localize answers name, a slash-separated path fs.ValidPath accepts, as a
// path of the operating system, or a path error of fs.ErrInvalid for
// operation, so nothing is written that the FS could not read back.
func localize(operation, name string) (string, error) {
	local, err := filepath.Localize(name)
	if err != nil {
		return "", &fs.PathError{Op: operation, Path: name, Err: fs.ErrInvalid}
	}
	return local, nil
}
