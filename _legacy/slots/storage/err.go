package storage

import (
	"github.com/dvoyni/cog/slots/storage/internal"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidMount reports a mount with an empty id or nil filesystem.
type ErrInvalidMount = internal.ErrInvalidMount

// ErrReservedMount reports an attempt to mount or unmount a reserved mount id.
// PermanentMount is derived from the permanent filesystem, so mounting it by
// hand could leave reads and writes pointing at different filesystems.
type ErrReservedMount = internal.ErrReservedMount

// ErrDuplicateMount reports a mount id contributed through ReadMountPort more
// than once. Plugins lists every contributor of Id, in plugin order, the same
// plugin repeated when it contributed the id twice. Plugin order is not a
// choice anyone makes, so storage refuses to let it pick a winner.
type ErrDuplicateMount = internal.ErrDuplicateMount

// ErrInvalidValuesPath reports a values file path that is not an fs.ValidPath.
type ErrInvalidValuesPath = internal.ErrInvalidValuesPath

// ErrNoWriteAccess reports a WriteFS with no permanent filesystem behind it,
// which happens when the handle passed to WriteAccess never had its write lock
// declared.
type ErrNoWriteAccess = internal.ErrNoWriteAccess

// ErrInvalidKey reports an empty value key.
type ErrInvalidKey = internal.ErrInvalidKey

// ErrInvalidValueRequest reports a zero AccessValuesRequest, which names no
// operation.
type ErrInvalidValueRequest = internal.ErrInvalidValueRequest

// ErrInvalidOutValue reports a nil destination pointer passed to GetValue.
type ErrInvalidOutValue = internal.ErrInvalidOutValue

// ErrInvalidValuesFile reports a values file that is not a JSON object. The
// file is left untouched so unreadable data is never overwritten.
type ErrInvalidValuesFile = internal.ErrInvalidValuesFile
