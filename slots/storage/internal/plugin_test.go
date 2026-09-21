package internal

import (
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/internal/types"
)

// storage is a Slot: it requires exactly one PermanentFS Adapter, so a
// composition without one fails before anything starts.
func TestStorageWithoutAnAdapterFailsWithErrMissingAdapter(t *testing.T) {
	var reported []error
	kernel.New(nil).Handler(func(err error) error {
		reported = append(reported, err)
		return err
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
	var reserved storage.ErrReservedMount
	err := startErr(mountPlugin{name: "mounts", mounts: []storage.ReadMount{
		{Id: storage.PermanentMount, Priority: 10, FS: fstest.MapFS{}},
	}})
	if !errors.As(err, &reserved) || reserved.Id != storage.PermanentMount {
		t.Fatalf("Start error = %v, want ErrReservedMount", err)
	}

	k := testKernel(t, storage.Config{}, newMemoryFS())
	if answer := k.ExecuteCommand[storage.SetMountCmd](storage.SetMountRequest{
		Mount: storage.ReadMount{Id: storage.PermanentMount, Priority: 1, FS: fstest.MapFS{}},
	}); !errors.As(answer.Err, &reserved) {
		t.Fatalf("SetMountCmd error = %v, want ErrReservedMount", answer.Err)
	}
	if answer := k.ExecuteCommand[storage.RemoveMountCmd](storage.RemoveMountRequest{Id: storage.PermanentMount}); !errors.As(answer.Err, &reserved) {
		t.Fatalf("RemoveMountCmd error = %v, want ErrReservedMount", answer.Err)
	}
}

// A read mount is a plain fs.FS a plugin contributes, and the permanent
// filesystem the Adapter provides reads back ahead of it.
func TestReadMountsAndTheAdapterShareOneOverlay(t *testing.T) {
	permanent := newMemoryFS()
	k := testKernel(t, storage.Config{}, permanent, mountPlugin{name: "assets", mounts: []storage.ReadMount{
		{Id: "assets", Priority: storage.DefaultReadPriority, FS: fstest.MapFS{
			"asset.txt": &fstest.MapFile{Data: []byte("asset")},
			"save.txt":  &fstest.MapFile{Data: []byte("packaged")},
		}},
	}})
	if err := permanent.WriteFile("save.txt", []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}

	assertRead(t, k, "asset.txt", "asset")
	assertRead(t, k, "save.txt", "saved")
}

// Mounts from several plugins, and several from one plugin, overlay by
// priority: the higher one answers a name both hold, and a name only the lower
// one holds still reads through it.
func TestContributedMountsOverlayByPriority(t *testing.T) {
	k := testKernel(t, storage.Config{}, newMemoryFS(),
		mountPlugin{name: "game", mounts: []storage.ReadMount{
			{Id: "res", Priority: storage.DefaultReadPriority, FS: fstest.MapFS{
				"shared.txt": &fstest.MapFile{Data: []byte("res")},
				"res.txt":    &fstest.MapFile{Data: []byte("res only")},
			}},
			{Id: "patch", Priority: 20, FS: fstest.MapFS{
				"patched.txt": &fstest.MapFile{Data: []byte("patch")},
			}},
		}},
		mountPlugin{name: "mod", mounts: []storage.ReadMount{
			{Id: "mod", Priority: 10, FS: fstest.MapFS{
				"shared.txt":  &fstest.MapFile{Data: []byte("mod")},
				"patched.txt": &fstest.MapFile{Data: []byte("mod")},
			}},
		}})

	assertRead(t, k, "shared.txt", "mod")
	assertRead(t, k, "res.txt", "res only")
	assertRead(t, k, "patched.txt", "patch")

	var ids []storage.MountId
	k.ExecuteCommand[readFileCmd](readFileRequest{read: func(filesystem storage.FileSystem) {
		for _, mount := range types.FileSystemMounts(filesystem) {
			ids = append(ids, mount.Id)
		}
	}})
	if want := []storage.MountId{storage.PermanentMount, "patch", "mod", "res"}; !slices.Equal(ids, want) {
		t.Fatalf("mounts = %v, want %v", ids, want)
	}
}

// Two contributions that claim one id are a composition mistake, whichever
// plugins they come from, so Start fails rather than letting plugin order pick
// a winner.
func TestADuplicateMountIdFailsStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plugins []kernel.Plugin
		want    []kernel.PluginName
	}{
		{name: "two plugins", plugins: []kernel.Plugin{
			mountPlugin{name: "a", mounts: []storage.ReadMount{{Id: "res", FS: fstest.MapFS{}}}},
			mountPlugin{name: "b", mounts: []storage.ReadMount{{Id: "res", FS: fstest.MapFS{}}}},
		}, want: []kernel.PluginName{"a", "b"}},
		{name: "one plugin", plugins: []kernel.Plugin{
			mountPlugin{name: "a", mounts: []storage.ReadMount{{Id: "res", FS: fstest.MapFS{}}, {Id: "res", FS: fstest.MapFS{}}}},
		}, want: []kernel.PluginName{"a", "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := startErr(tc.plugins...)

			var duplicate storage.ErrDuplicateMount
			if !errors.As(err, &duplicate) {
				t.Fatalf("Start error = %v, want ErrDuplicateMount", err)
			}
			if duplicate.Id != "res" || !slices.Equal(duplicate.Plugins, tc.want) {
				t.Fatalf("duplicate = %+v, want res from %v", duplicate, tc.want)
			}
		})
	}
}

func TestAnInvalidContributedMountFailsStart(t *testing.T) {
	for _, mount := range []storage.ReadMount{
		{Id: "", FS: fstest.MapFS{}},
		{Id: "res", FS: nil},
	} {
		err := startErr(mountPlugin{name: "mounts", mounts: []storage.ReadMount{mount}})

		var invalid storage.ErrInvalidMount
		if !errors.As(err, &invalid) || invalid.Id != mount.Id {
			t.Fatalf("Start error for %+v = %v, want ErrInvalidMount", mount, err)
		}
	}
}

// TestValueRoundTripThroughOneWriteLock is the point of the merge: one
// AccessValuesCmd reads the values file and flushes it back while holding a
// single lock, and a later read observes the write through the same overlay.
func TestValueRoundTripThroughOneWriteLock(t *testing.T) {
	permanent := newMemoryFS()
	k := testKernel(t, storage.Config{}, permanent)

	if answer := k.ExecuteCommand[storage.AccessValuesCmd](storage.SetValue("volume", 0.25)); answer.Err != nil {
		t.Fatal(answer.Err)
	}
	volume := 1.0
	response := k.ExecuteCommand[storage.AccessValuesCmd](storage.GetValue("volume", 1.0, &volume))
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	if !response.Found || volume != 0.25 {
		t.Fatalf("GetValue = %v (found %v), want 0.25 found", volume, response.Found)
	}
	if _, err := fs.Stat(permanent, storage.DefaultValuesPath); err != nil {
		t.Fatalf("values file not flushed to the permanent filesystem: %v", err)
	}
}

func TestAZeroValueRequestIsRejected(t *testing.T) {
	k := testKernel(t, storage.Config{}, newMemoryFS())
	var invalid storage.ErrInvalidValueRequest
	if answer := k.ExecuteCommand[storage.AccessValuesCmd](storage.AccessValuesRequest{}); !errors.As(answer.Err, &invalid) {
		t.Fatalf("zero request error = %v, want ErrInvalidValueRequest", answer.Err)
	}
}

func testKernel(t *testing.T, config storage.Config, permanent storage.PermanentFS, plugins ...kernel.Plugin) kernel.Executioner {
	t.Helper()
	engine := kernel.New(map[kernel.PluginName]any{storage.Name: config}).
		Handler(func(err error) error {
			t.Errorf("unexpected kernel error: %v", err)
			return err
		}).
		WithPlugins(append([]kernel.Plugin{New(), adapterPlugin{permanent: permanent}, readerPlugin{}}, plugins...)...)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	return engine.Executioner()
}

// startErr runs an engine over plugins and answers what Run returned, which is
// the Start failure when one plugin's Start fails.
func startErr(plugins ...kernel.Plugin) error {
	engine := kernel.New(nil).
		Handler(func(err error) error { return err }).
		WithPlugins(append([]kernel.Plugin{New(), adapterPlugin{permanent: newMemoryFS()}}, plugins...)...)
	done := make(chan error, 1)
	go func() { done <- engine.Run() }()
	<-engine.Ready()
	engine.Quit()
	return <-done
}

func assertRead(t *testing.T, k kernel.Executioner, name, want string) {
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

// mountPlugin contributes read mounts through storage's Port, in order.
type mountPlugin struct {
	name   kernel.PluginName
	mounts []storage.ReadMount
}

func (m mountPlugin) Name() kernel.PluginName         { return m.name }
func (mountPlugin) Dependencies() []kernel.PluginName { return nil }
func (m mountPlugin) Register(registrar *kernel.Registrar, _ any) error {
	for _, mount := range m.mounts {
		registrar.ProvideAdapter[testReadMount](mount)
	}
	return nil
}

// testReadMount is the Adapter mountPlugin contributes its mounts as.
type testReadMount kernel.Adapter[storage.ReadMountPort]

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
			}, func(_ kernel.Kernel, request readFileRequest) readFileResponse {
				request.read(filesystem.Get())
				return readFileResponse{}
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
