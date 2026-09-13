package storage

import (
	"github.com/dvoyni/cog/extensions/storage/internal"
	"github.com/dvoyni/cog/kernel"
)

// SetMountCmd adds or replaces one mount in the FileSystem overlay. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
// PermanentMount is reserved to the permanent filesystem.
type SetMountCmd kernel.Command[SetMountRequest, SetMountResponse]
type SetMountRequest struct{ Mount ReadMount }
type SetMountResponse struct{}

// RemoveMountCmd removes one mount from the FileSystem overlay by id.
// PermanentMount cannot be removed: it is derived from the permanent
// filesystem, and dropping it would leave written data unreadable.
type RemoveMountCmd kernel.Command[RemoveMountRequest, RemoveMountResponse]
type RemoveMountRequest struct{ Id MountId }
type RemoveMountResponse struct{ Removed bool }

// AccessValuesCmd is the single entry point to the key-value store. One
// command covers reads, writes, deletions and flushes because they are one
// operation from the store's side: each may load the values file, change the
// cache, and write it back, all under the same pair of locks. Build its request
// with GetValue, SetValue, DeleteValue or FlushValues.
type AccessValuesCmd kernel.Command[AccessValuesRequest, AccessValuesResponse]

// AccessValuesRequest is one operation on the key-value store. Its zero value
// names no operation.
type AccessValuesRequest = internal.AccessValuesRequest

// AccessValuesResponse reports whether the operation found its key.
type AccessValuesResponse = internal.AccessValuesResponse

// GetValue builds a request that decodes key into outValue, assigning
// defaultValue instead when the key is absent. A missing key stores nothing.
func GetValue[T any](key string, defaultValue T, outValue *T) AccessValuesRequest {
	return internal.GetValue(key, defaultValue, outValue)
}

// SetValue builds a request storing value under key.
func SetValue[T any](key string, value T) AccessValuesRequest {
	return internal.SetValue(key, value)
}

// SetValueNoFlush builds a request storing value under key, without writing it
// to the permanent filesystem until a later FlushValues.
func SetValueNoFlush[T any](key string, value T) AccessValuesRequest {
	return internal.SetValueNoFlush(key, value)
}

// DeleteValue builds a request removing key.
func DeleteValue(key string) AccessValuesRequest { return internal.DeleteValue(key) }

// DeleteValueNoFlush builds a request removing key, without writing the change
// to the permanent filesystem until a later FlushValues.
func DeleteValueNoFlush(key string) AccessValuesRequest { return internal.DeleteValueNoFlush(key) }

// FlushValues builds a request writing pending value changes to the permanent
// filesystem. It does nothing when no value changed since the last flush.
func FlushValues() AccessValuesRequest { return internal.FlushValues() }
