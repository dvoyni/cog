package storage

import "encoding/json"

// valueOp is one member of the union. Every operation owns its own validation
// and its own effect on the cached store, which keeps the handler a single
// load-apply-flush path instead of a switch that grows a case per operation.
type valueOp interface {
	// validate rejects a malformed request before any file is touched.
	validate() error
	// apply runs the operation against the cached store, loading the values file
	// through filesystem when it needs the entries. It returns the store to cache
	// even on failure, so a load that succeeded is never thrown away.
	apply(store valueStore, filesystem FileSystem) (valueStore, AccessValuesResponse, error)
	// flushes reports whether the result must reach disk before the command returns.
	flushes() bool
}

// GetValue builds a request that decodes key into outValue, assigning
// defaultValue instead when the key is absent. A missing key stores nothing.
func GetValue[T any](key string, defaultValue T, outValue *T) AccessValuesRequest {
	return AccessValuesRequest{op: getValueOp{
		key: key,
		decode: func(raw json.RawMessage) error {
			if outValue == nil {
				return ErrInvalidOutValue{Key: key}
			}
			if raw == nil {
				*outValue = defaultValue
				return nil
			}
			return json.Unmarshal(raw, outValue)
		},
	}}
}

// SetValue builds a request storing value under key.
func SetValue[T any](key string, value T) AccessValuesRequest {
	return AccessValuesRequest{op: setValueOp{
		key:     key,
		marshal: func() (json.RawMessage, error) { return json.Marshal(value) },
	}}
}

// SetValue builds a request storing value under key, will not automatically save to
// persistent storage
func SetValueNoFlush[T any](key string, value T) AccessValuesRequest {
	return AccessValuesRequest{op: setValueOp{
		key:       key,
		marshal:   func() (json.RawMessage, error) { return json.Marshal(value) },
		skipFlush: true,
	}}
}

// DeleteValue builds a request removing key. skipFlush defers the disk write to
// a later FlushValues.
func DeleteValue(key string) AccessValuesRequest {
	return AccessValuesRequest{op: deleteValueOp{key: key}}
}

// DeleteValue builds a request removing key, will not automatically save to
// persistent storage
func DeleteValueNoFlush(key string) AccessValuesRequest {
	return AccessValuesRequest{op: deleteValueOp{key: key, skipFlush: true}}
}

// FlushValues builds a request writing pending value changes to the permanent
// filesystem. It does nothing when no value changed since the last flush.
func FlushValues() AccessValuesRequest {
	return AccessValuesRequest{op: flushValuesOp{}}
}

type getValueOp struct {
	key string
	// decode receives the stored JSON, or nil when the key is absent.
	decode func(json.RawMessage) error
}

func (o getValueOp) validate() error {
	if o.key == "" {
		return ErrInvalidKey{}
	}
	return nil
}

func (o getValueOp) apply(
	store valueStore, filesystem FileSystem,
) (valueStore, AccessValuesResponse, error) {
	store, err := store.load(filesystem)
	if err != nil {
		return store, AccessValuesResponse{}, err
	}
	raw, found := store.entries[o.key]
	if !found {
		raw = nil
	}
	return store, AccessValuesResponse{Found: found}, o.decode(raw)
}

func (getValueOp) flushes() bool { return false }

type setValueOp struct {
	key       string
	marshal   func() (json.RawMessage, error)
	skipFlush bool
}

func (o setValueOp) validate() error {
	if o.key == "" {
		return ErrInvalidKey{}
	}
	return nil
}

func (o setValueOp) apply(
	store valueStore, filesystem FileSystem,
) (valueStore, AccessValuesResponse, error) {
	raw, err := o.marshal()
	if err != nil {
		return store, AccessValuesResponse{}, err
	}
	// Loading first keeps keys we have never read from being dropped by the flush.
	store, err = store.load(filesystem)
	if err != nil {
		return store, AccessValuesResponse{}, err
	}
	_, replaced := store.entries[o.key]
	store.entries[o.key] = raw
	store.dirty = true
	return store, AccessValuesResponse{Found: replaced}, nil
}

func (o setValueOp) flushes() bool { return !o.skipFlush }

type deleteValueOp struct {
	key       string
	skipFlush bool
}

func (o deleteValueOp) validate() error {
	if o.key == "" {
		return ErrInvalidKey{}
	}
	return nil
}

func (o deleteValueOp) apply(
	store valueStore, filesystem FileSystem,
) (valueStore, AccessValuesResponse, error) {
	store, err := store.load(filesystem)
	if err != nil {
		return store, AccessValuesResponse{}, err
	}
	_, existed := store.entries[o.key]
	if existed {
		delete(store.entries, o.key)
		store.dirty = true
	}
	return store, AccessValuesResponse{Found: existed}, nil
}

func (o deleteValueOp) flushes() bool { return !o.skipFlush }

// flushValuesOp touches no entry, so it never loads the values file: a store
// that was never read has nothing pending, and flush is a no-op on it.
type flushValuesOp struct{}

func (flushValuesOp) validate() error { return nil }

func (flushValuesOp) apply(
	store valueStore, _ FileSystem,
) (valueStore, AccessValuesResponse, error) {
	return store, AccessValuesResponse{}, nil
}

func (flushValuesOp) flushes() bool { return true }
