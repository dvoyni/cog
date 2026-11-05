package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// DefaultValuesFile is where the values are kept when Config names no
// ValuesFile.
const DefaultValuesFile = "config.json"

// Values is the resource of the key-value store, its values JSON, kept in a
// file in FS. Its methods take the FS they read and write: Get the FS, under
// its read lock, and Set, Delete and Flush the WriteFS, under its write lock,
// so the lock on FS guards the values and no reader sees the file mid-write.
// The file is read on the first call, through the overlay, so a read mount
// may ship the defaults, and kept: a missing file holds no values, and a
// corrupt one is an error from every call. Changes stay until a flush:
// Flush, or platform's own when a window closes.
type Values struct {
	// file is the values file's name in FS.
	file string
	// loading reads the file once, even from jobs reading at once.
	loading sync.Once
	entries map[string]json.RawMessage
	// loadError is why the file could not be read, answered by every call.
	loadError error
	// changed is whether entries differ from the file.
	changed bool
}

// Get answers the value stored under key, and whether there is one.
func (v *Values) Get(filesystem FS, key string) (value json.RawMessage, found bool, err error) {
	if err := v.load(filesystem); err != nil {
		return nil, false, err
	}
	value, found = v.entries[key]
	return value, found, nil
}

// Set stores value, which must be JSON, under key, kept until a flush.
func (v *Values) Set(filesystem WriteFS, key string, value json.RawMessage) error {
	if !json.Valid(value) {
		return fmt.Errorf("platform: the value set under %q is not JSON", key)
	}
	if err := v.load(filesystem); err != nil {
		return err
	}
	v.entries[key] = value
	v.changed = true
	return nil
}

// Delete removes key, kept until a flush. A missing key is no error.
func (v *Values) Delete(filesystem WriteFS, key string) error {
	if err := v.load(filesystem); err != nil {
		return err
	}
	if _, found := v.entries[key]; found {
		delete(v.entries, key)
		v.changed = true
	}
	return nil
}

// Flush writes the values to the values file in filesystem, when they changed
// since they were read or last flushed.
func (v *Values) Flush(filesystem WriteFS) error {
	if !v.changed {
		return nil
	}
	data, err := json.MarshalIndent(v.entries, "", "\t")
	if err != nil {
		return fmt.Errorf("platform: encoding the values: %w", err)
	}
	if err := filesystem.WriteFile(v.file, data, 0o644); err != nil {
		return fmt.Errorf("platform: writing the values to %s: %w", v.file, err)
	}
	v.changed = false
	return nil
}

// load reads the values file through filesystem on first use.
func (v *Values) load(filesystem FS) error {
	v.loading.Do(func() {
		data, err := fs.ReadFile(filesystem, v.file)
		if errors.Is(err, fs.ErrNotExist) {
			v.entries = map[string]json.RawMessage{}
			return
		}
		if err != nil {
			v.loadError = fmt.Errorf("platform: reading the values from %s: %w", v.file, err)
			return
		}
		if err := json.Unmarshal(data, &v.entries); err != nil {
			v.loadError = fmt.Errorf("platform: the values file %s is corrupt: %w", v.file, err)
			return
		}
		if v.entries == nil {
			v.entries = map[string]json.RawMessage{}
		}
	})
	return v.loadError
}
