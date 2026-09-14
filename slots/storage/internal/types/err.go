package types

import (
	"fmt"
)

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
