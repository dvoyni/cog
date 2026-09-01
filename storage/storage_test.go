package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestReadFSPriorityAndFallback(t *testing.T) {
	low := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("low")},
		"low.txt":    &fstest.MapFile{Data: []byte("fallback")},
	}
	high := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("high")},
	}
	readFS := newReadFS([]ReadMount{
		{Id: "low", Priority: 1, FS: low},
		{Id: "high", Priority: 10, FS: high},
	})

	assertReadFile(t, readFS, "shared.txt", "high")
	assertReadFile(t, readFS, "low.txt", "fallback")

	mounts := readFS.mountsSnapshot()
	if mounts[0].Id != "high" || mounts[1].Id != "low" {
		t.Fatalf("mount order = %v, want high then low", mounts)
	}
}

func TestReadFSStopsOnNonNotExistError(t *testing.T) {
	want := fs.ErrPermission
	readFS := newReadFS([]ReadMount{
		{Id: "blocked", Priority: 10, FS: errorFS{err: want}},
		{Id: "fallback", FS: fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("hidden")}}},
	})

	_, err := readFS.Open("file.txt")
	if !errors.Is(err, want) {
		t.Fatalf("Open error = %v, want %v", err, want)
	}
}

func TestWithReadDiskFSUsesPathAsMountId(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "asset.txt"), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFS, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig("test").
		WithReadDiskFS(MountId(directory)).
		WithWriteFS(writeFS)

	readFS, _, _, err := resolveConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := readFS.mount(MountId(directory)); !found {
		t.Fatalf("disk mount %q not found", directory)
	}
	assertReadFile(t, readFS, "asset.txt", "disk")
}

func TestDiskFSOperationsAndTraversal(t *testing.T) {
	writeFS, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFS.MkdirAll("saves", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFS.WriteFile("saves/one.json", []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFS.Rename("saves/one.json", "saves/two.json"); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(writeFS, "saves/two.json")
	if err != nil || string(data) != "one" {
		t.Fatalf("ReadFile = %q, %v; want one, nil", data, err)
	}
	// Callers write to a temp name and rename over the real one to make a save
	// atomic, so renaming onto an existing entry must replace it.
	if err := writeFS.WriteFile("saves/one.json", []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFS.Rename("saves/one.json", "saves/two.json"); err != nil {
		t.Fatal(err)
	}
	data, err = fs.ReadFile(writeFS, "saves/two.json")
	if err != nil || string(data) != "newer" {
		t.Fatalf("replaced ReadFile = %q, %v; want newer, nil", data, err)
	}
	if err := writeFS.Remove("saves/two.json"); err != nil {
		t.Fatal(err)
	}
	if err := writeFS.WriteFile("../escape", nil, 0o600); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("traversal error = %v, want fs.ErrInvalid", err)
	}
}

func TestValueStoreLoadDefaultsAndFlush(t *testing.T) {
	packaged := fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte(`{"volume":0.5,"name":"packaged"}`)},
	}
	writeFS, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	readFS := newReadFS([]ReadMount{
		{Id: "packaged", Priority: 1, FS: packaged},
		{Id: PermanentMount, Priority: 10, FS: writeFS},
	})

	store, err := (Values{path: DefaultValuesPath}).load(readFS)
	if err != nil {
		t.Fatal(err)
	}

	volume := 1.0
	if err := GetValue("volume", 1.0, &volume).apply(store.entries["volume"]); err != nil {
		t.Fatal(err)
	}
	if volume != 0.5 {
		t.Fatalf("volume = %v, want 0.5", volume)
	}

	missing := 7
	if err := GetValue("missing", 7, &missing).apply(nil); err != nil {
		t.Fatal(err)
	}
	if missing != 7 || len(store.entries) != 2 {
		t.Fatalf("missing key = %v with %d entries, want default and no insertion", missing, len(store.entries))
	}

	raw, err := SetValue("volume", 0.25, false).marshal()
	if err != nil {
		t.Fatal(err)
	}
	store.entries["volume"] = raw
	store.dirty = true
	store, err = store.flush(writeFS)
	if err != nil {
		t.Fatal(err)
	}
	if store.dirty {
		t.Fatal("store still dirty after flush")
	}
	if _, err := fs.Stat(writeFS, DefaultValuesPath+".tmp"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary file survived flush: %v", err)
	}

	reloaded, err := (Values{path: DefaultValuesPath}).load(readFS)
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.entries["volume"]) != "0.25" {
		t.Fatalf("reloaded volume = %s, want 0.25", reloaded.entries["volume"])
	}
	if string(reloaded.entries["name"]) != `"packaged"` {
		t.Fatalf("reloaded name = %s, want packaged value preserved", reloaded.entries["name"])
	}
}

func TestValueStoreMissingAndCorruptFiles(t *testing.T) {
	empty, err := (Values{path: DefaultValuesPath}).load(newReadFS(nil))
	if err != nil {
		t.Fatalf("missing values file error = %v, want nil", err)
	}
	if empty.entries == nil || len(empty.entries) != 0 {
		t.Fatalf("entries = %v, want empty non-nil map", empty.entries)
	}

	corrupt := newReadFS([]ReadMount{{Id: "corrupt", FS: fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte("{not json")},
	}}})
	store, err := (Values{path: DefaultValuesPath}).load(corrupt)
	var invalid ErrInvalidValuesFile
	if !errors.As(err, &invalid) {
		t.Fatalf("corrupt values file error = %v, want ErrInvalidValuesFile", err)
	}
	if store.entries != nil {
		t.Fatal("corrupt values file was cached")
	}
}

func assertReadFile(t *testing.T, filesystem fs.FS, name, want string) {
	t.Helper()
	data, err := fs.ReadFile(filesystem, name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", name, data, want)
	}
}

type errorFS struct{ err error }

func (e errorFS) Open(string) (fs.File, error) { return nil, e.err }
