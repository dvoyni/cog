//go:build js

package filesystems_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"syscall/js"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
)

// localStorageKey answers a key of the page's localStorage for the test
// alone, removed when the test ends.
func localStorageKey(t *testing.T) string {
	t.Helper()
	key := "cog.test." + t.Name()
	storage := js.Global().Get("localStorage")
	if storage.IsUndefined() {
		t.Fatal("the page has no localStorage; run node with --localstorage-file")
	}
	storage.Call("removeItem", key)
	t.Cleanup(func() { storage.Call("removeItem", key) })
	return key
}

// newLocalStorageWriteFS answers the LocalStorageWriteFS under key.
func newLocalStorageWriteFS(t *testing.T, key string) *filesystems.LocalStorageWriteFS {
	t.Helper()
	writable, err := filesystems.NewLocalStorageWriteFS(key)
	if err != nil {
		t.Fatal(err)
	}
	return writable
}

// bindLocalStorage answers a bind of writable as the writable filesystem
// and of mounts as read mounts.
func bindLocalStorage(writable *filesystems.LocalStorageWriteFS, mounts ...fs.FS) func(c *kernel.Configurator) {
	return func(c *kernel.Configurator) {
		c.BindSocket[platform.WriteFSSocket](writable)
		for _, mount := range mounts {
			c.BindSocket(platform.ReadMountSocket{FS: mount})
		}
	}
}

func TestWritesToLocalStorageReadBackAndPassTestFS(t *testing.T) {
	key := localStorageKey(t)
	mount := fstest.MapFS{"readme": {Data: []byte("mounted")}, "textures/grass.png": {Data: []byte("grass")}}
	h := spawn(t, bindLocalStorage(newLocalStorageWriteFS(t, key), mount))
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		for _, err := range []error{
			filesystem.MkdirAll("saves/old", 0o755),
			filesystem.MkdirAll("empty", 0o700),
			filesystem.WriteFile("saves/slot1", []byte("one"), 0o644),
			filesystem.AppendFile("saves/slot1", []byte("+two"), 0o600),
			filesystem.AppendFile("saves/log", []byte("started"), 0o600),
			filesystem.WriteFile("saves/temporary", []byte("two"), 0o644),
			filesystem.Rename("saves/temporary", "saves/slot2"),
			filesystem.Remove("saves/old"),
			filesystem.WriteFile("readme", []byte("written"), 0o644),
			filesystem.MkdirAll("levels/1", 0o755),
			filesystem.WriteFile("levels/1/map", []byte("map"), 0o644),
			filesystem.Rename("levels", "worlds"),
		} {
			if err != nil {
				t.Error(err)
				return
			}
		}
	})
	if string(mount["readme"].Data) != "mounted" {
		t.Error("a write reached the read mount")
	}

	h.respawn(bindLocalStorage(newLocalStorageWriteFS(t, key), mount))
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		if err := fstest.TestFS(filesystem, "saves/slot1", "saves/slot2", "saves/log", "readme", "worlds/1/map", "textures/grass.png", "empty"); err != nil {
			t.Error(err)
		}
		for name, want := range map[string]string{"saves/slot1": "one+two", "saves/slot2": "two", "saves/log": "started", "readme": "written", "worlds/1/map": "map"} {
			if data, err := fs.ReadFile(filesystem, name); err != nil || string(data) != want {
				t.Errorf("a new filesystem on the key reads %s as %q, %v, want %q", name, data, err, want)
			}
		}
		for name, want := range map[string]fs.FileMode{"saves/slot1": 0o644, "saves/log": 0o600, "empty": fs.ModeDir | 0o700} {
			if info, err := fs.Stat(filesystem, name); err != nil || info.Mode() != want {
				t.Errorf("%s stats %v, %v, want mode %v", name, info, err, want)
			}
		}
		for _, name := range []string{"saves/old", "saves/temporary", "levels", "levels/1/map"} {
			if _, err := fs.Stat(filesystem, name); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s stats %v, want it gone", name, err)
			}
		}
	})
}

func TestLocalStorageRefusesWhatADiskWould(t *testing.T) {
	writable := newLocalStorageWriteFS(t, localStorageKey(t))
	for _, err := range []error{
		writable.WriteFile("kept", []byte("kept"), 0o644),
		writable.MkdirAll("folder/inner", 0o755),
		writable.WriteFile("folder/file", []byte("file"), 0o644),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for operation, test := range map[string]struct {
		err  error
		want error
	}{
		"writing into a missing folder":        {writable.WriteFile("missing/slot", []byte("save"), 0o644), fs.ErrNotExist},
		"appending into a missing folder":      {writable.AppendFile("missing/log", []byte("save"), 0o644), fs.ErrNotExist},
		"writing over a folder":                {writable.WriteFile("folder", []byte("save"), 0o644), fs.ErrExist},
		"making a folder under a file":         {writable.MkdirAll("kept/folder", 0o755), fs.ErrExist},
		"removing a missing name":              {writable.Remove("missing"), fs.ErrNotExist},
		"removing a folder not empty":          {writable.Remove("folder"), fs.ErrExist},
		"renaming a missing name":              {writable.Rename("missing", "moved"), fs.ErrNotExist},
		"renaming into a missing folder":       {writable.Rename("kept", "missing/kept"), fs.ErrNotExist},
		"renaming a folder into itself":        {writable.Rename("folder", "folder/inner/folder"), fs.ErrInvalid},
		"renaming a file over a folder":        {writable.Rename("kept", "folder"), fs.ErrExist},
		"renaming a folder over a file":        {writable.Rename("folder/inner", "kept"), fs.ErrExist},
		"renaming a folder into a missing one": {writable.Rename("folder/inner", "missing/inner"), fs.ErrNotExist},
		"writing ./slot":                       {writable.WriteFile("./slot", []byte("save"), 0o644), fs.ErrInvalid},
		"opening ../kept":                      {openError(writable, "../kept"), fs.ErrInvalid},
	} {
		var pathError *fs.PathError
		if !errors.Is(test.err, test.want) || !errors.As(test.err, &pathError) {
			t.Errorf("%s fails with %v, want a path error of %v", operation, test.err, test.want)
		}
	}
	if err := fstest.TestFS(writable, "kept", "folder/file", "folder/inner"); err != nil {
		t.Error(err)
	}
}

func TestValuesFlushToLocalStorageAndReadBack(t *testing.T) {
	key := localStorageKey(t)
	defaults := fstest.MapFS{"config.json": {Data: []byte(`{"volume": 0.8}`)}}
	h := spawn(t, bindLocalStorage(newLocalStorageWriteFS(t, key), defaults))
	h.writing(func(filesystem platform.WriteFS, values *platform.Values) {
		if value, _, err := values.Get(filesystem, "volume"); err != nil || string(value) != "0.8" {
			t.Errorf("volume gets %s, %v, want the read mount's 0.8", value, err)
		}
		if err := values.Set(filesystem, "volume", json.RawMessage(`0.5`)); err != nil {
			t.Error(err)
		}
		if err := values.Flush(filesystem); err != nil {
			t.Error(err)
		}
	})
	if js.Global().Get("localStorage").Call("getItem", key).IsNull() {
		t.Fatal("the flush left the key empty")
	}

	h.respawn(bindLocalStorage(newLocalStorageWriteFS(t, key), defaults))
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if value, _, err := values.Get(filesystem, "volume"); err != nil || string(value) != "0.5" {
			t.Errorf("a new kernel gets volume %s, %v, want the flushed 0.5", value, err)
		}
	})
}

func TestACorruptLocalStorageKeyFailsEveryCall(t *testing.T) {
	key := localStorageKey(t)
	js.Global().Get("localStorage").Call("setItem", key, "{not json")
	writable := newLocalStorageWriteFS(t, key)
	if err := openError(writable, "kept"); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening under a corrupt key fails with %v, want the corruption", err)
	}
	if err := writable.WriteFile("kept", []byte("kept"), 0o644); err == nil {
		t.Error("writing under a corrupt key succeeded")
	}
	if got := js.Global().Get("localStorage").Call("getItem", key).String(); got != "{not json" {
		t.Errorf("a failed write replaced the corrupt key with %q", got)
	}
}

func TestLocalStorageNeedsAKeyAndAPageWithIt(t *testing.T) {
	if _, err := filesystems.NewLocalStorageWriteFS(""); err == nil {
		t.Error("an empty key is accepted")
	}
	object := js.Global().Get("Object")
	descriptor := object.Call("getOwnPropertyDescriptor", js.Global(), "localStorage")
	object.Call("defineProperty", js.Global(), "localStorage", map[string]any{"value": js.Undefined(), "configurable": true})
	defer object.Call("defineProperty", js.Global(), "localStorage", descriptor)
	if _, err := filesystems.NewLocalStorageWriteFS("cog.test.missing"); err == nil {
		t.Error("a page without localStorage is accepted")
	}
}

// openError answers why opening name in filesystem fails, or nil.
func openError(filesystem fs.FS, name string) error {
	file, err := filesystem.Open(name)
	if err == nil {
		file.Close()
	}
	return err
}

func TestAWriteOverTheQuotaFailsAndKeepsTheFiles(t *testing.T) {
	writable := newLocalStorageWriteFS(t, localStorageKey(t))
	if err := writable.WriteFile("kept", []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writable.WriteFile("huge", make([]byte, 64<<20), 0o644); err == nil {
		t.Fatal("64 MiB fit in localStorage")
	}
	if data, err := fs.ReadFile(writable, "kept"); err != nil || string(data) != "kept" {
		t.Errorf("kept reads %q, %v after a refused write, want it kept", data, err)
	}
	if _, err := fs.Stat(writable, "huge"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused file stats %v, want it missing", err)
	}
}
