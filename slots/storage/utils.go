package storage

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage/internal/types"
)

// GetValue builds a request that decodes key into outValue, assigning
// defaultValue instead when the key is absent. A missing key stores nothing.
func GetValue[T any](key string, defaultValue T, outValue *T) AccessValuesRequest {
	return types.GetValue[T](key, defaultValue, outValue)
}

// SetValue builds a request storing value under key.
func SetValue[T any](key string, value T) AccessValuesRequest {
	return types.SetValue[T](key, value)
}

// SetValueNoFlush builds a request storing value under key, without writing it
// to the permanent filesystem until a later FlushValues.
func SetValueNoFlush[T any](key string, value T) AccessValuesRequest {
	return types.SetValueNoFlush[T](key, value)
}

// DeleteValue builds a request removing key.
func DeleteValue(key string) AccessValuesRequest {
	return types.DeleteValue(key)
}

// DeleteValueNoFlush builds a request removing key, without writing the change
// to the permanent filesystem until a later FlushValues.
func DeleteValueNoFlush(key string) AccessValuesRequest {
	return types.DeleteValueNoFlush(key)
}

// FlushValues builds a request writing pending value changes to the permanent
// filesystem. It does nothing when no value changed since the last flush.
func FlushValues() AccessValuesRequest {
	return types.FlushValues()
}

// WriteAccess turns a write lock on FileSystem into write access. Demanding the
// kernel.Write handle is the entire mechanism: a read lock cannot produce one,
// so a mutation under a shared lock does not compile. Reads stay on the
// FileSystem value itself, which the same handle also yields through Get.
func WriteAccess(handle kernel.Write[FileSystem]) WriteFS {
	return types.WriteAccess(handle)
}

// NewFileSystem builds a FileSystem over a single mounted filesystem, with no
// permanent filesystem behind it. The plugin wires the real resource from
// configured mounts at runtime; this constructor is for tests and embedders
// that need a standalone reader over an fs.FS.
func NewFileSystem(id MountId, filesystem fs.FS) FileSystem {
	return types.NewStandaloneFileSystem(id, filesystem)
}
