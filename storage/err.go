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

// ErrInvalidValuesPath reports a values file path that is not an fs.ValidPath.
type ErrInvalidValuesPath struct{ Path string }

func (e ErrInvalidValuesPath) Error() string {
	return fmt.Sprintf("storage: invalid values path %q", e.Path)
}

// ErrInvalidKey reports an empty value key.
type ErrInvalidKey struct{}

func (ErrInvalidKey) Error() string { return "storage: invalid empty value key" }

// ErrInvalidValueRequest reports a value request that did not come from
// GetValue or SetValue.
type ErrInvalidValueRequest struct{ Key string }

func (e ErrInvalidValueRequest) Error() string {
	return fmt.Sprintf("storage: uninitialized value request for key %q", e.Key)
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
