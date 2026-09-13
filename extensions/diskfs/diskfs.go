//go:build !js

package diskfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dvoyni/cog/extensions/storage"
)

// openDiskFS creates path if needed and returns a confined writable filesystem
// rooted there. Names accepted by its methods use fs.ValidPath form.
func openDiskFS(path string) (storage.PermanentFS, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("diskfs: resolve permanent directory %q: %w", path, err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("diskfs: create permanent directory %q: %w", absolute, err)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("diskfs: open permanent directory %q: %w", absolute, err)
	}
	if err := root.Close(); err != nil {
		return nil, fmt.Errorf("diskfs: close permanent directory %q: %w", absolute, err)
	}
	return diskFS{path: absolute}, nil
}

type diskFS struct{ path string }

func (d diskFS) Open(name string) (fs.File, error) {
	if err := validatePath("open", name); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, err
	}
	file, openErr := root.Open(name)
	closeErr := root.Close()
	if openErr != nil {
		return nil, openErr
	}
	if closeErr != nil {
		file.Close()
		return nil, closeErr
	}
	return file, nil
}

func (d diskFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := validatePath("write", name); err != nil {
		return err
	}
	return d.withRoot(func(root *os.Root) error { return root.WriteFile(name, data, perm) })
}

func (d diskFS) MkdirAll(path string, perm fs.FileMode) error {
	if err := validatePath("mkdir", path); err != nil {
		return err
	}
	return d.withRoot(func(root *os.Root) error { return root.MkdirAll(path, perm) })
}

func (d diskFS) Remove(name string) error {
	if err := validatePath("remove", name); err != nil {
		return err
	}
	return d.withRoot(func(root *os.Root) error { return root.Remove(name) })
}

func (d diskFS) Rename(oldName, newName string) error {
	if err := validatePath("rename", oldName); err != nil {
		return err
	}
	if err := validatePath("rename", newName); err != nil {
		return err
	}
	return d.withRoot(func(root *os.Root) error { return root.Rename(oldName, newName) })
}

func (d diskFS) withRoot(operation func(*os.Root) error) error {
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return err
	}
	operationErr := operation(root)
	closeErr := root.Close()
	if operationErr != nil {
		return operationErr
	}
	return closeErr
}

func validatePath(operation, path string) error {
	if fs.ValidPath(path) {
		return nil
	}
	return &fs.PathError{Op: operation, Path: path, Err: fs.ErrInvalid}
}

var _ storage.PermanentFS = diskFS{}
