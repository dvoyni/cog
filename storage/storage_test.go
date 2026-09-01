package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
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

	readFS, _, err := resolveConfig(config)
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

func TestCommandsExposeAndOperateOnResources(t *testing.T) {
	writeFS, err := OpenDiskFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig("test").
		WithReadFS("assets", 10, fstest.MapFS{"asset.txt": &fstest.MapFile{Data: []byte("asset")}}).
		WithWriteFS(writeFS)
	engine := kernel.New(map[kernel.PluginName]any{Name: config}).
		Handler(func(err error) bool {
			t.Errorf("unexpected kernel error: %v", err)
			return false
		}).
		WithPlugins(New())
	k := engine.Executioner()

	read, err := k.ExecuteCommand[ReadFileCmd](ReadFileRequest{Name: "asset.txt"})
	if err != nil || string(read.Data) != "asset" {
		t.Fatalf("ReadFileCmd = %q, %v; want asset, nil", read.Data, err)
	}
	var callbackData []byte
	_, err = k.ExecuteCommand[GetReadFSCmd](GetReadFSRequest{Use: func(filesystem fs.FS) error {
		var err error
		callbackData, err = fs.ReadFile(filesystem, "asset.txt")
		return err
	}})
	if err != nil || string(callbackData) != "asset" {
		t.Fatalf("GetReadFSCmd = %q, %v; want asset, nil", callbackData, err)
	}
	wantCallbackErr := errors.New("callback failed")
	_, err = k.ExecuteCommand[GetReadFSCmd](GetReadFSRequest{Use: func(fs.FS) error {
		return wantCallbackErr
	}})
	if !errors.Is(err, wantCallbackErr) {
		t.Fatalf("callback error = %v, want %v", err, wantCallbackErr)
	}
	_, err = k.ExecuteCommand[GetReadFSCmd](GetReadFSRequest{})
	var invalidCallback ErrInvalidReadFSCallback
	if !errors.As(err, &invalidCallback) {
		t.Fatalf("nil callback error = %v, want ErrInvalidReadFSCallback", err)
	}
	_, err = k.ExecuteCommand[WriteFileCmd](WriteFileRequest{Name: "save.json", Data: []byte("save"), Perm: 0o600})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := k.ExecuteCommand[ReadFileCmd](ReadFileRequest{Name: "save.json"})
	if err != nil || string(saved.Data) != "save" {
		t.Fatalf("saved data = %q, %v; want save, nil", saved.Data, err)
	}
	// The permanent filesystem is mounted at the highest priority, so a written
	// file is what reads back even when a read-only mount has the same name.
	_, err = k.ExecuteCommand[WriteFileCmd](WriteFileRequest{Name: "asset.txt", Data: []byte("overridden"), Perm: 0o600})
	if err != nil {
		t.Fatal(err)
	}
	overridden, err := k.ExecuteCommand[ReadFileCmd](ReadFileRequest{Name: "asset.txt"})
	if err != nil || string(overridden.Data) != "overridden" {
		t.Fatalf("overridden asset = %q, %v; want overridden, nil", overridden.Data, err)
	}
	_, err = k.ExecuteCommand[ReadFileCmd](ReadFileRequest{Name: "missing"})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing error = %v, want fs.ErrNotExist", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.Run(ctx)
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
