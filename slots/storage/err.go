package storage

import (
	"fmt"

	"github.com/dvoyni/cog/slots/storage/internal/types"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("storage: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidMount reports a mount with an empty id or nil filesystem.
type ErrInvalidMount struct{ Id MountId }

func (e ErrInvalidMount) Error() string {
	return fmt.Sprintf("storage: invalid read mount %q", e.Id)
}

// ErrReservedMount reports an attempt to mount or unmount a reserved mount id.
// PermanentMount is derived from the permanent filesystem, so mounting it by
// hand could leave reads and writes pointing at different filesystems.
type ErrReservedMount struct{ Id MountId }

func (e ErrReservedMount) Error() string {
	return fmt.Sprintf("storage: read mount %q is reserved to the permanent filesystem", e.Id)
}

// ErrInvalidValuesPath reports a values file path that is not an fs.ValidPath.
type ErrInvalidValuesPath struct{ Path string }

func (e ErrInvalidValuesPath) Error() string {
	return fmt.Sprintf("storage: invalid values path %q", e.Path)
}

// ErrNoWriteAccess reports a WriteFS with no permanent filesystem behind it,
// which happens when the handle passed to WriteAccess never had its write lock
// declared.
type ErrNoWriteAccess = types.ErrNoWriteAccess

// ErrInvalidKey reports an empty value key.
type ErrInvalidKey = types.ErrInvalidKey

// ErrInvalidValueRequest reports a zero AccessValuesRequest, which names no
// operation.
type ErrInvalidValueRequest = types.ErrInvalidValueRequest

// ErrInvalidOutValue reports a nil destination pointer passed to GetValue.
type ErrInvalidOutValue = types.ErrInvalidOutValue

// ErrInvalidValuesFile reports a values file that is not a JSON object. The
// file is left untouched so unreadable data is never overwritten.
type ErrInvalidValuesFile = types.ErrInvalidValuesFile
