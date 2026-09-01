package storage

import (
	"fmt"
)

// ErrInvalidConfig reports a non-Config plugin configuration value.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("storage: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidMount reports a mount with an empty id or nil filesystem.
type ErrInvalidMount struct{ Id MountId }

func (e ErrInvalidMount) Error() string {
	return fmt.Sprintf("storage: invalid read mount %q", e.Id)
}

// ErrInvalidAppId reports an application id that is not a single directory name.
type ErrInvalidAppId struct{ AppId string }

func (e ErrInvalidAppId) Error() string {
	return fmt.Sprintf("storage: invalid app id %q", e.AppId)
}

// ErrInvalidWriteFS reports an attempt to install a nil permanent filesystem.
type ErrInvalidWriteFS struct{}

func (ErrInvalidWriteFS) Error() string { return "storage: invalid nil write filesystem" }
