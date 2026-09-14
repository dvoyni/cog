package storageimpl

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// storage is a Port: it requires exactly one PermanentFS Adapter, so a
// composition without one fails before anything starts.
func TestStorageWithoutAnAdapterFailsWithErrMissingAdapter(t *testing.T) {
	var reported []error
	kernel.New(nil).Handler(func(err error) bool {
		reported = append(reported, err)
		return true
	}).WithPlugins(New())

	var missing kernel.ErrMissingAdapter
	if !errors.As(errors.Join(reported...), &missing) {
		t.Fatalf("composition reported %v, want ErrMissingAdapter", reported)
	}
	if missing.Plugin != storage.Name || missing.Port != reflect.TypeFor[storage.PermanentFSPort]() {
		t.Fatalf("missing adapter = %+v, want storage's PermanentFSPort", missing)
	}
}

func TestPermanentMountIsReserved(t *testing.T) {
	config := DefaultConfig().WithReadFS(storage.PermanentMount, 10, fstest.MapFS{})

	var reserved storage.ErrReservedMount
	if _, _, err := resolveConfig(config, nil); !errors.As(err, &reserved) {
		t.Fatalf("resolveConfig error = %v, want ErrReservedMount", err)
	}

	k := testKernel(t, DefaultConfig(), newMemoryFS())
	if _, err := k.ExecuteCommand[storage.SetMountCmd](storage.SetMountRequest{
		Mount: storage.ReadMount{Id: storage.PermanentMount, Priority: 1, FS: fstest.MapFS{}},
	}); !errors.As(err, &reserved) {
		t.Fatalf("SetMountCmd error = %v, want ErrReservedMount", err)
	}
	if _, err := k.ExecuteCommand[storage.RemoveMountCmd](storage.RemoveMountRequest{Id: storage.PermanentMount}); !errors.As(err, &reserved) {
		t.Fatalf("RemoveMountCmd error = %v, want ErrReservedMount", err)
	}
}

// A read mount is a plain fs.FS the composition root chooses, and the
// permanent filesystem the Adapter provides reads back ahead of it.
func TestReadMountsAndTheAdapterShareOneOverlay(t *testing.T) {
	permanent := newMemoryFS()
	config := DefaultConfig().WithReadFS("assets", storage.DefaultReadPriority, fstest.MapFS{
		"asset.txt": &fstest.MapFile{Data: []byte("asset")},
		"save.txt":  &fstest.MapFile{Data: []byte("packaged")},
	})
	k := testKernel(t, config, permanent)
	if err := permanent.WriteFile("save.txt", []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}

	assertRead := func(name, want string) {
		t.Helper()
		var data []byte
		var err error
		k.ExecuteCommand[readFileCmd](readFileRequest{read: func(filesystem storage.FileSystem) {
			data, err = fs.ReadFile(filesystem, name)
		}})
		if err != nil || string(data) != want {
			t.Fatalf("ReadFile(%q) = %q, %v; want %q", name, data, err, want)
		}
	}
	assertRead("asset.txt", "asset")
	assertRead("save.txt", "saved")
}

// TestValueRoundTripThroughOneWriteLock is the point of the merge: one
// AccessValuesCmd reads the values file and flushes it back while holding a
// single lock, and a later read observes the write through the same overlay.
func TestValueRoundTripThroughOneWriteLock(t *testing.T) {
	permanent := newMemoryFS()
	k := testKernel(t, DefaultConfig(), permanent)

	if _, err := k.ExecuteCommand[storage.AccessValuesCmd](storage.SetValue("volume", 0.25)); err != nil {
		t.Fatal(err)
	}
	volume := 1.0
	response, err := k.ExecuteCommand[storage.AccessValuesCmd](storage.GetValue("volume", 1.0, &volume))
	if err != nil {
		t.Fatal(err)
	}
	if !response.Found || volume != 0.25 {
		t.Fatalf("GetValue = %v (found %v), want 0.25 found", volume, response.Found)
	}
	if _, err := fs.Stat(permanent, storage.DefaultValuesPath); err != nil {
		t.Fatalf("values file not flushed to the permanent filesystem: %v", err)
	}
}

func TestAZeroValueRequestIsRejected(t *testing.T) {
	k := testKernel(t, DefaultConfig(), newMemoryFS())
	var invalid storage.ErrInvalidValueRequest
	if _, err := k.ExecuteCommand[storage.AccessValuesCmd](storage.AccessValuesRequest{}); !errors.As(err, &invalid) {
		t.Fatalf("zero request error = %v, want ErrInvalidValueRequest", err)
	}
}

func testKernel(t *testing.T, config Config, permanent storage.PermanentFS) kernel.Executioner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine := kernel.New(map[kernel.PluginName]any{storage.Name: config}).
		Handler(func(err error) bool {
			t.Errorf("unexpected kernel error: %v", err)
			return true
		}).
		WithPlugins(New(), adapterPlugin{permanent: permanent}, readerPlugin{})
	go engine.Run(ctx)
	<-engine.Ready()
	return engine.Executioner()
}

// adapterPlugin provides the PermanentFS Adapter the tests compose.
type adapterPlugin struct{ permanent storage.PermanentFS }

func (adapterPlugin) Name() kernel.PluginName           { return "storage-test-adapter" }
func (adapterPlugin) Dependencies() []kernel.PluginName { return nil }
func (a adapterPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testPermanentFS](a.permanent)
	return nil
}

// testPermanentFS is the Adapter this fixture fills storage's permanent
// filesystem Port as.
type testPermanentFS kernel.Adapter[storage.PermanentFSPort]

// readFileCmd reads the FileSystem resource under a read lock.
type readFileCmd kernel.Command[readFileRequest, readFileResponse]
type readFileRequest struct{ read func(storage.FileSystem) }
type readFileResponse struct{}

type readerPlugin struct{}

func (readerPlugin) Name() kernel.PluginName { return "storage-test-reader" }
func (readerPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{storage.Name}
}
func (readerPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[readFileCmd](func() (kernel.Lock, kernel.Execute[readFileRequest, readFileResponse]) {
		var filesystem kernel.Read[storage.FileSystem]
		return func(access kernel.ResourceAccess) {
				filesystem = access.GetRead[storage.FileSystem]()
			}, func(_ kernel.Kernel, request readFileRequest) (readFileResponse, error) {
				request.read(filesystem.Get())
				return readFileResponse{}, nil
			}
	})
	return nil
}

// memoryFS is an in-memory PermanentFS. Directories are implied by the files
// under them, as fstest.MapFS implies them.
type memoryFS struct {
	mu    sync.Mutex
	files fstest.MapFS
}

func newMemoryFS() *memoryFS { return &memoryFS{files: fstest.MapFS{}} }

func (m *memoryFS) Open(name string) (fs.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files.Open(name)
}

func (m *memoryFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = &fstest.MapFile{Data: append([]byte(nil), data...), Mode: perm}
	return nil
}

func (m *memoryFS) MkdirAll(string, fs.FileMode) error { return nil }

func (m *memoryFS) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; !ok {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	delete(m.files, name)
	return nil
}

func (m *memoryFS) Rename(oldName, newName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[oldName]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrNotExist}
	}
	delete(m.files, oldName)
	m.files[newName] = file
	return nil
}
