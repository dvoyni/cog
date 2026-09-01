//go:build !js

package storage

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

func defaultReadMount() (ReadMount, bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return ReadMount{}, false, fmt.Errorf("storage: resolve executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return ReadMount{}, false, fmt.Errorf("storage: resolve executable symlinks: %w", err)
	}
	return ReadMount{
		Id:       ExecutableMount,
		Priority: math.MinInt,
		FS:       os.DirFS(filepath.Dir(executable)),
	}, true, nil
}

func defaultWriteFS(appId string) (WriteFS, error) {
	path, err := permanentDir(appId)
	if err != nil {
		return nil, err
	}
	return OpenDiskFS(path)
}

func permanentDir(appId string) (string, error) {
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("storage: resolve user data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("storage: resolve user data directory: %w", err)
		}
	}
	return filepath.Join(base, appId), nil
}

// OpenDiskFS creates path if needed and returns a confined writable filesystem
// rooted there. Names accepted by its methods use fs.ValidPath form.
func OpenDiskFS(path string) (WriteFS, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve permanent directory %q: %w", path, err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("storage: create permanent directory %q: %w", absolute, err)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("storage: open permanent directory %q: %w", absolute, err)
	}
	if err := root.Close(); err != nil {
		return nil, fmt.Errorf("storage: close permanent directory %q: %w", absolute, err)
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

var _ WriteFS = diskFS{}
