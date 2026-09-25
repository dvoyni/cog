package internal

import (
	"errors"
	"io/fs"
	"math"
	"testing"
	"testing/fstest"
)

func TestFileSystemPriorityAndFallback(t *testing.T) {
	low := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("low")},
		"low.txt":    &fstest.MapFile{Data: []byte("fallback")},
	}
	high := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("high")},
	}
	filesystem := NewFileSystem([]ReadMount{
		{Id: "low", Priority: 1, FS: low},
		{Id: "high", Priority: 10, FS: high},
	}, nil)

	assertReadFile(t, filesystem, "shared.txt", "high")
	assertReadFile(t, filesystem, "low.txt", "fallback")

	mounts := FileSystemMounts(filesystem)
	if mounts[0].Id != "high" || mounts[1].Id != "low" {
		t.Fatalf("mount order = %v, want high then low", mounts)
	}
}

func TestFileSystemStopsOnNonNotExistError(t *testing.T) {
	want := fs.ErrPermission
	filesystem := NewFileSystem([]ReadMount{
		{Id: "blocked", Priority: 10, FS: errorFS{err: want}},
		{Id: "fallback", FS: fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("hidden")}}},
	}, nil)

	_, err := filesystem.Open("file.txt")
	if !errors.Is(err, want) {
		t.Fatalf("Open error = %v, want %v", err, want)
	}
}

// The permanent filesystem is an Adapter bound after the resource is built, so
// the overlay resolves it when it reads and writes, not when it is constructed.
func TestThePermanentFilesystemIsResolvedWhenUsed(t *testing.T) {
	var bound PermanentFS
	filesystem := NewFileSystem(nil, func() PermanentFS { return bound })

	bound = typesNewMemoryFS()
	if err := writeAccess(filesystem).WriteFile("save.txt", []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertReadFile(t, filesystem, "save.txt", "saved")
}

func TestValueStoreLoadDefaultsAndFlush(t *testing.T) {
	packaged := fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte(`{"volume":0.5,"name":"packaged"}`)},
	}
	permanent := typesNewMemoryFS()
	filesystem := NewFileSystem([]ReadMount{{Id: "packaged", Priority: 1, FS: packaged}}, constant(permanent))
	write := writeAccess(filesystem)

	store, err := NewValues("config.json").load(filesystem)
	if err != nil {
		t.Fatal(err)
	}

	volume := 1.0
	store, response, err := GetValue("volume", 1.0, &volume).op.apply(store, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	if volume != 0.5 || !response.Found {
		t.Fatalf("volume = %v (found %v), want 0.5 found", volume, response.Found)
	}

	missing := 7
	store, response, err = GetValue("missing", 7, &missing).op.apply(store, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 7 || response.Found || len(store.entries) != 2 {
		t.Fatalf("missing key = %v (found %v) with %d entries, want default and no insertion",
			missing, response.Found, len(store.entries))
	}

	store, _, err = SetValueNoFlush("volume", 0.25).op.apply(store, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	store, err = store.flush(write)
	if err != nil {
		t.Fatal(err)
	}
	if store.dirty {
		t.Fatal("store still dirty after flush")
	}
	if _, err := fs.Stat(permanent, "config.json.tmp"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary file survived flush: %v", err)
	}

	reloaded, err := NewValues("config.json").load(filesystem)
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
	empty, err := NewValues("config.json").load(NewFileSystem(nil, nil))
	if err != nil {
		t.Fatalf("missing values file error = %v, want nil", err)
	}
	if empty.entries == nil || len(empty.entries) != 0 {
		t.Fatalf("entries = %v, want empty non-nil map", empty.entries)
	}

	corrupt := NewFileSystem([]ReadMount{{Id: "corrupt", FS: fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte("{not json")},
	}}}, nil)
	store, err := NewValues("config.json").load(corrupt)
	var invalid ErrInvalidValuesFile
	if !errors.As(err, &invalid) {
		t.Fatalf("corrupt values file error = %v, want ErrInvalidValuesFile", err)
	}
	if store.entries != nil {
		t.Fatal("corrupt values file was cached")
	}
}

// TestPermanentMountOutranksMaxIntMounts pins the tie-break: canvas mounts its
// shaders at math.MaxInt, so a permanent mount that merely shares that priority
// would let another mount shadow data the caller just wrote.
func TestPermanentMountOutranksMaxIntMounts(t *testing.T) {
	permanent := typesNewMemoryFS()
	if err := permanent.WriteFile("shared.txt", []byte("permanent"), 0o600); err != nil {
		t.Fatal(err)
	}
	rival := fstest.MapFS{"shared.txt": &fstest.MapFile{Data: []byte("rival")}}

	filesystem := NewFileSystem([]ReadMount{{Id: "rival", Priority: math.MaxInt, FS: rival}}, constant(permanent))

	if got := FileSystemMounts(filesystem)[0].Id; got != PermanentMount {
		t.Fatalf("first mount = %q, want %q", got, PermanentMount)
	}
	assertReadFile(t, filesystem, "shared.txt", "permanent")
}

// A mount added or removed later keeps the permanent filesystem at the head of
// the search order and behind WriteAccess.
func TestChangingMountsKeepsThePermanentFilesystem(t *testing.T) {
	permanent := typesNewMemoryFS()
	filesystem := NewFileSystem(nil, constant(permanent))
	filesystem = FileSystemWithMount(filesystem, ReadMount{Id: "extra", Priority: math.MaxInt, FS: fstest.MapFS{
		"save.txt": &fstest.MapFile{Data: []byte("extra")},
	}})
	if err := writeAccess(filesystem).WriteFile("save.txt", []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertReadFile(t, filesystem, "save.txt", "saved")

	filesystem = FileSystemWithoutMount(filesystem, "extra")
	if _, found := FileSystemMount(filesystem, "extra"); found {
		t.Fatal("removed mount still present")
	}
	assertReadFile(t, filesystem, "save.txt", "saved")
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

func constant(permanent PermanentFS) func() PermanentFS {
	return func() PermanentFS { return permanent }
}

// memoryFS is an in-memory PermanentFS. Directories are implied by the files
// under them, as fstest.MapFS implies them.
type typesMemoryFS struct{ files fstest.MapFS }

func typesNewMemoryFS() *typesMemoryFS { return &typesMemoryFS{files: fstest.MapFS{}} }

func (m *typesMemoryFS) Open(name string) (fs.File, error) { return m.files.Open(name) }

func (m *typesMemoryFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	m.files[name] = &fstest.MapFile{Data: append([]byte(nil), data...), Mode: perm}
	return nil
}

func (m *typesMemoryFS) MkdirAll(string, fs.FileMode) error { return nil }

func (m *typesMemoryFS) Remove(name string) error {
	if _, ok := m.files[name]; !ok {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	delete(m.files, name)
	return nil
}

func (m *typesMemoryFS) Rename(oldName, newName string) error {
	file, ok := m.files[oldName]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrNotExist}
	}
	delete(m.files, oldName)
	m.files[newName] = file
	return nil
}
