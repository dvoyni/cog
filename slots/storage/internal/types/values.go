package types

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path"
)

// Values caches the values file. entries is nil until the first load, which
// distinguishes "not read yet" from "read and empty".
type Values struct {
	path    string
	entries map[string]json.RawMessage
	dirty   bool
}

// NewValues returns an empty, unloaded store over the values file at path.
func NewValues(path string) Values { return Values{path: path} }

// load reads the values file once. A missing file yields an empty store; a
// malformed one is reported without caching, so a later flush cannot overwrite
// data we failed to understand.
func (v Values) load(filesystem FileSystem) (Values, error) {
	if v.entries != nil {
		return v, nil
	}
	data, err := fs.ReadFile(filesystem, v.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return v, err
		}
		v.entries = map[string]json.RawMessage{}
		return v, nil
	}
	entries := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return v, ErrInvalidValuesFile{Path: v.path, Err: err}
	}
	v.entries = entries
	return v, nil
}

// flush writes the cache through a temporary file and renames it over the
// values file, so an interrupted write cannot truncate the previous contents.
func (v Values) flush(writeFS WriteFS) (Values, error) {
	if !v.dirty {
		return v, nil
	}
	data, err := json.Marshal(v.entries)
	if err != nil {
		return v, err
	}
	if parent := path.Dir(v.path); parent != "." {
		if err := writeFS.MkdirAll(parent, 0o700); err != nil {
			return v, err
		}
	}
	temporary := v.path + ".tmp"
	if err := writeFS.WriteFile(temporary, data, 0o600); err != nil {
		return v, err
	}
	if err := writeFS.Rename(temporary, v.path); err != nil {
		_ = writeFS.Remove(temporary)
		return v, err
	}
	v.dirty = false
	return v, nil
}
