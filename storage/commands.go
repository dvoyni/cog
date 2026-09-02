package storage

import (
	"encoding/json"

	"github.com/dvoyni/cog/kernel"
)

// SetMountCmd adds or replaces one mount in the FileSystem overlay. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
// PermanentMount is reserved to SetPermanentFSCmd.
type SetMountCmd kernel.Command[SetMountRequest, SetMountResponse]
type SetMountRequest struct{ Mount ReadMount }
type SetMountResponse struct{}

// RemoveMountCmd removes one mount from the FileSystem overlay by id.
// PermanentMount cannot be removed: it is derived from the permanent
// filesystem, and dropping it would leave written data unreadable.
type RemoveMountCmd kernel.Command[RemoveMountRequest, RemoveMountResponse]
type RemoveMountRequest struct{ Id MountId }
type RemoveMountResponse struct{ Removed bool }

// SetPermanentFSCmd replaces the permanent filesystem. It swaps the write
// target and the PermanentMount overlay entry in one step, so reads never fall
// through to the filesystem that was just replaced.
type SetPermanentFSCmd kernel.Command[SetPermanentFSRequest, SetPermanentFSResponse]
type SetPermanentFSRequest struct{ FS PermanentFS }
type SetPermanentFSResponse struct{}

// GetValueCmd reads one value from the key-value store, loading the values file
// through FileSystem on first use. A missing key leaves the caller's default in
// place and stores nothing.
type GetValueCmd kernel.Command[GetValueRequest, GetValueResponse]

// GetValueRequest is opaque: build it with GetValue so the default and the
// destination are guaranteed to share one type.
type GetValueRequest struct {
	key string
	// apply receives the stored JSON, or nil when the key is absent.
	apply func(json.RawMessage) error
}

type GetValueResponse struct{ Found bool }

// GetValue builds a request that decodes key into outValue, assigning
// defaultValue instead when the key is absent.
func GetValue[T any](key string, defaultValue T, outValue *T) GetValueRequest {
	return GetValueRequest{
		key: key,
		apply: func(raw json.RawMessage) error {
			if outValue == nil {
				return ErrInvalidOutValue{Key: key}
			}
			if raw == nil {
				*outValue = defaultValue
				return nil
			}
			return json.Unmarshal(raw, outValue)
		},
	}
}

// SetValueCmd stores one value, flushing unless SkipFlush batches the write.
type SetValueCmd kernel.Command[SetValueRequest, SetValueResponse]

// SetValueRequest is opaque: build it with SetValue.
type SetValueRequest struct {
	key       string
	marshal   func() (json.RawMessage, error)
	skipFlush bool
}

type SetValueResponse struct{}

// SetValue builds a request storing value under key. skipFlush defers the disk
// write to a later FlushValuesCmd.
func SetValue[T any](key string, value T, skipFlush bool) SetValueRequest {
	return SetValueRequest{
		key:       key,
		marshal:   func() (json.RawMessage, error) { return json.Marshal(value) },
		skipFlush: skipFlush,
	}
}

// DeleteValueCmd removes one key, flushing unless SkipFlush batches the write.
type DeleteValueCmd kernel.Command[DeleteValueRequest, DeleteValueResponse]
type DeleteValueRequest struct {
	Key       string
	SkipFlush bool
}
type DeleteValueResponse struct{ Existed bool }

// FlushValuesCmd writes pending value changes to the permanent filesystem. It
// does nothing when no value changed since the last flush.
type FlushValuesCmd kernel.Command[FlushValuesRequest, FlushValuesResponse]
type FlushValuesRequest struct{}
type FlushValuesResponse struct{}
