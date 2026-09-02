package storage

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
)

func TestFileSystemPriorityAndFallback(t *testing.T) {
	low := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("low")},
		"low.txt":    &fstest.MapFile{Data: []byte("fallback")},
	}
	high := fstest.MapFS{
		"shared.txt": &fstest.MapFile{Data: []byte("high")},
	}
	filesystem := newFileSystem([]ReadMount{
		{Id: "low", Priority: 1, FS: low},
		{Id: "high", Priority: 10, FS: high},
	}, nil)

	assertReadFile(t, filesystem, "shared.txt", "high")
	assertReadFile(t, filesystem, "low.txt", "fallback")

	mounts := filesystem.mountsSnapshot()
	if mounts[0].Id != "high" || mounts[1].Id != "low" {
		t.Fatalf("mount order = %v, want high then low", mounts)
	}
}

func TestFileSystemStopsOnNonNotExistError(t *testing.T) {
	want := fs.ErrPermission
	filesystem := newFileSystem([]ReadMount{
		{Id: "blocked", Priority: 10, FS: errorFS{err: want}},
		{Id: "fallback", FS: fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("hidden")}}},
	}, nil)

	_, err := filesystem.Open("file.txt")
	if !errors.Is(err, want) {
		t.Fatalf("Open error = %v, want %v", err, want)
	}
}

func TestWithReadDiskFSUsesPathAsMountId(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "asset.txt"), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig("test").
		WithReadDiskFS(MountId(directory)).
		WithPermanentFS(permanent)

	filesystem, _, err := resolveConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := filesystem.mount(MountId(directory)); !found {
		t.Fatalf("disk mount %q not found", directory)
	}
	assertReadFile(t, filesystem, "asset.txt", "disk")
}

func TestDiskFSOperationsAndTraversal(t *testing.T) {
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := permanent.MkdirAll("saves", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := permanent.WriteFile("saves/one.json", []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := permanent.Rename("saves/one.json", "saves/two.json"); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(permanent, "saves/two.json")
	if err != nil || string(data) != "one" {
		t.Fatalf("ReadFile = %q, %v; want one, nil", data, err)
	}
	// Callers write to a temp name and rename over the real one to make a save
	// atomic, so renaming onto an existing entry must replace it.
	if err := permanent.WriteFile("saves/one.json", []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := permanent.Rename("saves/one.json", "saves/two.json"); err != nil {
		t.Fatal(err)
	}
	data, err = fs.ReadFile(permanent, "saves/two.json")
	if err != nil || string(data) != "newer" {
		t.Fatalf("replaced ReadFile = %q, %v; want newer, nil", data, err)
	}
	if err := permanent.Remove("saves/two.json"); err != nil {
		t.Fatal(err)
	}
	if err := permanent.WriteFile("../escape", nil, 0o600); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("traversal error = %v, want fs.ErrInvalid", err)
	}
}

func TestValueStoreLoadDefaultsAndFlush(t *testing.T) {
	packaged := fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte(`{"volume":0.5,"name":"packaged"}`)},
	}
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystem := newFileSystem([]ReadMount{{Id: "packaged", Priority: 1, FS: packaged}}, permanent)
	write := writeFS{permanent: permanent}

	store, err := (Values{path: DefaultValuesPath}).load(filesystem)
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
	store, err = store.flush(write)
	if err != nil {
		t.Fatal(err)
	}
	if store.dirty {
		t.Fatal("store still dirty after flush")
	}
	if _, err := fs.Stat(permanent, DefaultValuesPath+".tmp"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary file survived flush: %v", err)
	}

	reloaded, err := (Values{path: DefaultValuesPath}).load(filesystem)
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
	empty, err := (Values{path: DefaultValuesPath}).load(newFileSystem(nil, nil))
	if err != nil {
		t.Fatalf("missing values file error = %v, want nil", err)
	}
	if empty.entries == nil || len(empty.entries) != 0 {
		t.Fatalf("entries = %v, want empty non-nil map", empty.entries)
	}

	corrupt := newFileSystem([]ReadMount{{Id: "corrupt", FS: fstest.MapFS{
		"config.json": &fstest.MapFile{Data: []byte("{not json")},
	}}}, nil)
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

// TestPermanentMountOutranksMaxIntMounts pins the tie-break: canvas mounts its
// shaders at math.MaxInt, so a permanent mount that merely shares that priority
// would let another mount shadow data the caller just wrote.
func TestPermanentMountOutranksMaxIntMounts(t *testing.T) {
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := permanent.WriteFile("shared.txt", []byte("permanent"), 0o600); err != nil {
		t.Fatal(err)
	}
	rival := fstest.MapFS{"shared.txt": &fstest.MapFile{Data: []byte("rival")}}

	filesystem := newFileSystem([]ReadMount{{Id: "rival", Priority: math.MaxInt, FS: rival}}, permanent)

	if got := filesystem.mountsSnapshot()[0].Id; got != PermanentMount {
		t.Fatalf("first mount = %q, want %q", got, PermanentMount)
	}
	assertReadFile(t, filesystem, "shared.txt", "permanent")
}

// TestWithPermanentSwapsReadAndWriteTogether covers what the single resource
// buys: replacing the permanent filesystem moves the write target and the
// overlay entry that reads it back in one step, so reads cannot keep falling
// through to the filesystem that was just replaced.
func TestWithPermanentSwapsReadAndWriteTogether(t *testing.T) {
	before, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := before.WriteFile("save.txt", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := after.WriteFile("save.txt", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	filesystem := newFileSystem(nil, before).withPermanent(after)

	assertReadFile(t, filesystem, "save.txt", "new")
	if err := (writeFS{permanent: filesystem.permanent}).WriteFile("probe.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(after, "probe.txt"); err != nil {
		t.Fatalf("write landed outside the new permanent filesystem: %v", err)
	}
	assertReadFile(t, filesystem, "probe.txt", "x")
}

func TestPermanentMountIsReserved(t *testing.T) {
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig("test").
		WithPermanentFS(permanent).
		WithReadFS(PermanentMount, 10, fstest.MapFS{})

	var reserved ErrReservedMount
	if _, _, err := resolveConfig(config); !errors.As(err, &reserved) {
		t.Fatalf("resolveConfig error = %v, want ErrReservedMount", err)
	}

	k := testKernel(t, DefaultConfig("test").WithPermanentFS(permanent))
	if _, err := k.ExecuteCommand[SetMountCmd](SetMountRequest{
		Mount: ReadMount{Id: PermanentMount, Priority: 1, FS: fstest.MapFS{}},
	}); !errors.As(err, &reserved) {
		t.Fatalf("SetMountCmd error = %v, want ErrReservedMount", err)
	}
	if _, err := k.ExecuteCommand[RemoveMountCmd](RemoveMountRequest{Id: PermanentMount}); !errors.As(err, &reserved) {
		t.Fatalf("RemoveMountCmd error = %v, want ErrReservedMount", err)
	}
}

// TestWriteAccessRequiresBoundHandle covers the failure mode of the capability:
// a handle that never had its write lock declared has no backend, and must say
// so rather than panic or silently write nothing.
func TestWriteAccessRequiresBoundHandle(t *testing.T) {
	var unbound kernel.Write[FileSystem]
	write := WriteAccess(unbound)

	var noAccess ErrNoWriteAccess
	if err := write.WriteFile("save.txt", nil, 0o600); !errors.As(err, &noAccess) {
		t.Fatalf("WriteFile error = %v, want ErrNoWriteAccess", err)
	}
	if err := write.Rename("a", "b"); !errors.As(err, &noAccess) {
		t.Fatalf("Rename error = %v, want ErrNoWriteAccess", err)
	}
}

// TestValueRoundTripThroughOneWriteLock is the point of the merge: SetValueCmd
// reads the values file and flushes it back while holding a single lock, and a
// later read observes the write through the same overlay.
func TestValueRoundTripThroughOneWriteLock(t *testing.T) {
	permanent, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := testKernel(t, DefaultConfig("test").WithPermanentFS(permanent))

	if _, err := k.ExecuteCommand[SetValueCmd](SetValue("volume", 0.25, false)); err != nil {
		t.Fatal(err)
	}
	volume := 1.0
	response, err := k.ExecuteCommand[GetValueCmd](GetValue("volume", 1.0, &volume))
	if err != nil {
		t.Fatal(err)
	}
	if !response.Found || volume != 0.25 {
		t.Fatalf("GetValue = %v (found %v), want 0.25 found", volume, response.Found)
	}
	if _, err := fs.Stat(permanent, DefaultValuesPath); err != nil {
		t.Fatalf("values file not flushed to the permanent filesystem: %v", err)
	}
}

func testKernel(t *testing.T, config Config) kernel.Executioner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine := kernel.New(map[kernel.PluginName]any{Name: config}).
		Handler(func(err error) bool {
			t.Errorf("unexpected kernel error: %v", err)
			return true
		}).
		WithPlugins(New())
	go engine.Run(ctx)
	<-engine.Ready()
	return engine.Executioner()
}
