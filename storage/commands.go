package storage

import (
	"encoding/json"

	"github.com/dvoyni/cog/kernel"
)

// SetReadFSCmd adds or replaces one mount in the ReadFS resource. Mounts are
// replaced by id; changing a mount's priority also updates its search order.
type SetReadFSCmd kernel.Command[SetReadFSRequest, SetReadFSResponse]
type SetReadFSRequest struct{ Mount ReadMount }
type SetReadFSResponse struct{}

// RemoveReadFSCmd removes one mount from the ReadFS resource by id.
type RemoveReadFSCmd kernel.Command[RemoveReadFSRequest, RemoveReadFSResponse]
type RemoveReadFSRequest struct{ Id MountId }
type RemoveReadFSResponse struct{ Removed bool }

// SetWriteFSCmd replaces the permanent WriteFS resource.
type SetWriteFSCmd kernel.Command[SetWriteFSRequest, SetWriteFSResponse]
type SetWriteFSRequest struct{ FS WriteFS }
type SetWriteFSResponse struct{}

// GetValueCmd reads one value from the key-value store, loading the values file
// through ReadFS on first use. A missing key leaves the caller's default in
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

// FlushValuesCmd writes pending value changes to WriteFS. It does nothing when
// no value changed since the last flush.
type FlushValuesCmd kernel.Command[FlushValuesRequest, FlushValuesResponse]
type FlushValuesRequest struct{}
type FlushValuesResponse struct{}
