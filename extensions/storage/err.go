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

// ErrInvalidPermanentFS reports an attempt to install a nil permanent filesystem.
type ErrInvalidPermanentFS struct{}

func (ErrInvalidPermanentFS) Error() string { return "storage: invalid nil permanent filesystem" }

// ErrReservedMount reports an attempt to mount or unmount a reserved mount id.
// PermanentMount is derived from the permanent filesystem, so mounting it by
// hand could leave reads and writes pointing at different filesystems.
type ErrReservedMount struct{ Id MountId }

func (e ErrReservedMount) Error() string {
	return fmt.Sprintf("storage: read mount %q is reserved to the permanent filesystem", e.Id)
}

// ErrNoWriteAccess reports a WriteFS with no permanent filesystem behind it,
// which happens when the handle passed to WriteAccess never had its write lock
// declared. It is a programming error, reported rather than panicked so the
// failing operation is named.
type ErrNoWriteAccess struct {
	Op   string
	Path string
}

func (e ErrNoWriteAccess) Error() string {
	return fmt.Sprintf("storage: %s %q without write access to the permanent filesystem", e.Op, e.Path)
}

// ErrInvalidValuesPath reports a values file path that is not an fs.ValidPath.
type ErrInvalidValuesPath struct{ Path string }

func (e ErrInvalidValuesPath) Error() string {
	return fmt.Sprintf("storage: invalid values path %q", e.Path)
}

// ErrInvalidKey reports an empty value key.
type ErrInvalidKey struct{}

func (ErrInvalidKey) Error() string { return "storage: invalid empty value key" }

// ErrInvalidValueRequest reports a zero AccessValuesRequest, which names no
// operation because it did not come from GetValue, SetValue, DeleteValue or
// FlushValues.
type ErrInvalidValueRequest struct{}

func (ErrInvalidValueRequest) Error() string {
	return "storage: value request names no operation"
}

// ErrInvalidOutValue reports a nil destination pointer passed to GetValue.
type ErrInvalidOutValue struct{ Key string }

func (e ErrInvalidOutValue) Error() string {
	return fmt.Sprintf("storage: nil out value for key %q", e.Key)
}

// ErrInvalidValuesFile reports a values file that is not a JSON object. The
// file is left untouched so unreadable data is never overwritten.
type ErrInvalidValuesFile struct {
	Path string
	Err  error
}

func (e ErrInvalidValuesFile) Error() string {
	return fmt.Sprintf("storage: parse values file %q: %v", e.Path, e.Err)
}

func (e ErrInvalidValuesFile) Unwrap() error { return e.Err }
