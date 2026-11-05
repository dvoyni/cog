package platform_test

import (
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

func TestOverlayPassesTestFS(t *testing.T) {
	f := newFake()
	f.writable.MapFS["saves/slot1"] = &fstest.MapFile{Data: []byte("save")}
	f.mounts = []platform.ReadMountSocket{
		{fstest.MapFS{"textures/grass.png": {Data: []byte("grass")}, "readme": {Data: []byte("low")}}, 0},
		{fstest.MapFS{"textures/stone.png": {Data: []byte("stone")}, "readme": {Data: []byte("high")}}, 1},
	}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		if err := fstest.TestFS(filesystem, "saves/slot1", "textures/grass.png", "textures/stone.png", "readme"); err != nil {
			t.Error(err)
		}
	})
}

func TestReadDirMergesLayersAndTheHigherLayerWinsAName(t *testing.T) {
	f := newFake()
	f.writable.MapFS["levels/3.json"] = &fstest.MapFile{Data: []byte("written")}
	f.writable.MapFS["docs"] = &fstest.MapFile{Data: []byte("a file over a folder")}
	f.mounts = []platform.ReadMountSocket{
		{fstest.MapFS{
			"levels/1.json": {Data: []byte("low")},
			"levels/2.json": {Data: []byte("low")},
			"levels/extra":  {Data: []byte("a file under a folder")},
			"docs/hidden":   {Data: []byte("under a file")},
		}, 0},
		{fstest.MapFS{
			"levels/2.json":  {Data: []byte("high")},
			"levels/extra/a": {Data: []byte("high")},
		}, 1},
	}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		entries, err := fs.ReadDir(filesystem, "levels")
		if err != nil {
			t.Error(err)
			return
		}
		var listing []string
		for _, entry := range entries {
			listing = append(listing, entry.Name()+map[bool]string{true: "/"}[entry.IsDir()])
		}
		if want := []string{"1.json", "2.json", "3.json", "extra/"}; !slices.Equal(listing, want) {
			t.Errorf("levels lists %v, want %v", listing, want)
		}
		if data, _ := fs.ReadFile(filesystem, "levels/2.json"); string(data) != "high" {
			t.Errorf("levels/2.json reads %q, want the higher mount's", data)
		}
		var walked []string
		err = fs.WalkDir(filesystem, ".", func(name string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				walked = append(walked, name)
			}
			return err
		})
		if err != nil {
			t.Error(err)
			return
		}
		want := []string{"docs", "levels/1.json", "levels/2.json", "levels/3.json", "levels/extra/a"}
		if !slices.Equal(walked, want) {
			t.Errorf("walking the overlay finds %v, want %v", walked, want)
		}
		if _, err := fs.ReadDir(filesystem, "docs"); err == nil {
			t.Error("docs, a file in the writable layer, lists as a folder")
		}
	})
}

func TestWritableLayerThenMountsByPriorityInRegistrationOrder(t *testing.T) {
	f := newFake()
	f.mounts = []platform.ReadMountSocket{
		{fstest.MapFS{"a": {Data: []byte("first at 5")}, "b": {Data: []byte("zero")}}, 5},
		{fstest.MapFS{"a": {Data: []byte("zero")}, "b": {Data: []byte("zero")}, "c": {Data: []byte("zero")}}, 0},
		{fstest.MapFS{"a": {Data: []byte("second at 5")}, "b": {Data: []byte("second at 5")}}, 5},
	}
	f.writable.MapFS["b"] = &fstest.MapFile{Data: []byte("written")}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		for name, want := range map[string]string{"a": "first at 5", "b": "written", "c": "zero"} {
			if data, err := fs.ReadFile(filesystem, name); err != nil || string(data) != want {
				t.Errorf("%s reads %q, %v, want %q", name, data, err, want)
			}
			file, err := filesystem.Open(name)
			if err != nil {
				t.Error(err)
				return
			}
			info, _ := file.Stat()
			file.Close()
			if stat, _ := fs.Stat(filesystem, name); info.Size() != int64(len(want)) || stat.Size() != info.Size() {
				t.Errorf("%s opens or stats a layer other than the one it reads", name)
			}
		}
		if _, err := filesystem.Open("missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("opening a name in no layer fails with %v, want it not to exist", err)
		}
	})
}

func TestLayersBoundAfterPlatformAreSearched(t *testing.T) {
	f := newFake()
	f.installWritable = false
	h := &harness{t: t, fake: f}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	writable := &memoryFileSystem{MapFS: fstest.MapFS{"saves/slot1": {Data: []byte("save")}}}
	late := &secondInstaller{func(c *kernel.Configurator) {
		c.BindSocket[platform.WriteFSSocket](writable)
		c.BindSocket(platform.ReadMountSocket{FS: fstest.MapFS{"readme": {Data: []byte("late")}}})
	}}
	id, err := h.supervisor.Spawn(nil, 2, f, platform.NewPlugin(), h, late)
	if err != nil {
		t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		for name, want := range map[string]string{"saves/slot1": "save", "readme": "late"} {
			if data, err := fs.ReadFile(filesystem, name); err != nil || string(data) != want {
				t.Errorf("%s reads %q, %v, want %q", name, data, err, want)
			}
		}
	})
}

func TestWritesLandInTheWritableLayerAndReadBack(t *testing.T) {
	f := newFake()
	mount := fstest.MapFS{"readme": {Data: []byte("mounted")}}
	f.mounts = []platform.ReadMountSocket{{mount, 0}}
	h := spawn(t, f)
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		for _, err := range []error{
			filesystem.MkdirAll("saves/old", 0o755),
			filesystem.WriteFile("saves/slot1", []byte("one"), 0o644),
			filesystem.AppendFile("saves/slot1", []byte("+two"), 0o644),
			filesystem.WriteFile("saves/temporary", []byte("two"), 0o644),
			filesystem.Rename("saves/temporary", "saves/slot2"),
			filesystem.Remove("saves/old"),
			filesystem.WriteFile("readme", []byte("written"), 0o644),
		} {
			if err != nil {
				t.Error(err)
				return
			}
		}
		if data, _ := fs.ReadFile(filesystem, "saves/slot1"); string(data) != "one+two" {
			t.Errorf("the write handle reads saves/slot1 as %q", data)
		}
	})
	written := slices.Sorted(maps.Keys(f.writable.MapFS))
	if want := []string{"readme", "saves", "saves/slot1", "saves/slot2"}; !slices.Equal(written, want) {
		t.Errorf("the writable layer holds %v, want %v", written, want)
	}
	if string(mount["readme"].Data) != "mounted" {
		t.Error("a write reached a read mount")
	}
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		if _, writes := filesystem.(platform.WriteFSSocket); writes {
			t.Error("Files under the read lock asserts into writing")
		}
		for name, want := range map[string]string{"saves/slot1": "one+two", "saves/slot2": "two", "readme": "written"} {
			if data, _ := fs.ReadFile(filesystem, name); string(data) != want {
				t.Errorf("%s reads back %q, want %q", name, data, want)
			}
		}
	})
}

func TestAMountImplementingOnlyOpenIsReadInFull(t *testing.T) {
	f := newFake()
	f.mounts = []platform.ReadMountSocket{{FS: openOnly{fstest.MapFS{"a/b": {Data: []byte("b")}, "c": {Data: []byte("c")}}}}}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		if err := fstest.TestFS(filesystem, "a/b", "c"); err != nil {
			t.Error(err)
		}
	})
}

// openOnly is an fs.FS that implements nothing beyond Open.
type openOnly struct{ filesystem fstest.MapFS }

func (o openOnly) Open(name string) (fs.File, error) { return o.filesystem.Open(name) }

// memoryFileSystem is an in-memory writable filesystem.
type memoryFileSystem struct{ fstest.MapFS }

func (m *memoryFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
	}
	m.MapFS[name] = &fstest.MapFile{Data: slices.Clone(data), Mode: perm}
	return nil
}

func (m *memoryFileSystem) MkdirAll(name string, perm fs.FileMode) error {
	for ; name != "."; name = path.Dir(name) {
		if _, found := m.MapFS[name]; !found {
			m.MapFS[name] = &fstest.MapFile{Mode: fs.ModeDir | perm}
		}
	}
	return nil
}

func (m *memoryFileSystem) Remove(name string) error {
	if _, found := m.MapFS[name]; !found {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	delete(m.MapFS, name)
	return nil
}

func (m *memoryFileSystem) Rename(oldName, newName string) error {
	file, found := m.MapFS[oldName]
	if !found {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrNotExist}
	}
	delete(m.MapFS, oldName)
	m.MapFS[newName] = file
	return nil
}

func (m *memoryFileSystem) AppendFile(name string, data []byte, perm fs.FileMode) error {
	file, found := m.MapFS[name]
	if !found {
		return m.WriteFile(name, data, perm)
	}
	file.Data = append(file.Data, data...)
	return nil
}

func TestWithoutAWritableFilesystemReadsSearchTheMountsAndWritesFail(t *testing.T) {
	f := newFake()
	f.installWritable = false
	f.mounts = []platform.ReadMountSocket{{FS: fstest.MapFS{"config.json": {Data: []byte(`{"volume": 0.8}`)}, "readme": {Data: []byte("mounted")}}}}
	h := spawn(t, f)
	h.writing(func(filesystem platform.WriteFS, values *platform.Values) {
		if err := fstest.TestFS(filesystem, "config.json", "readme"); err != nil {
			t.Error(err)
		}
		for operation, err := range map[string]error{
			"WriteFile":  filesystem.WriteFile("saves/slot1", []byte("save"), 0o644),
			"MkdirAll":   filesystem.MkdirAll("saves", 0o755),
			"Remove":     filesystem.Remove("readme"),
			"Rename":     filesystem.Rename("readme", "moved"),
			"AppendFile": filesystem.AppendFile("log", []byte("started"), 0o644),
		} {
			if err != platform.ErrNoWriteFS {
				t.Errorf("%s fails with %v, want ErrNoWriteFS", operation, err)
			}
		}
		if value, _, err := values.Get(filesystem, "volume"); err != nil || string(value) != "0.8" {
			t.Errorf("volume gets %s, %v, want the read mount's 0.8", value, err)
		}
		if err := values.Set(filesystem, "volume", []byte("0.5")); err != nil {
			t.Error(err)
		}
		if err := values.Flush(filesystem); !errors.Is(err, platform.ErrNoWriteFS) {
			t.Errorf("flushing a change fails with %v, want ErrNoWriteFS", err)
		}
	})
	if logged := slices.Clone(h.errors); len(logged) != 0 {
		t.Errorf("the kernel logged %v", logged)
	}
}

func TestWithNoFilesystemBoundEveryNameIsMissing(t *testing.T) {
	f := newFake()
	f.installWritable = false
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if _, err := fs.ReadFile(filesystem, "readme"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("reading readme fails with %v, want it not to exist", err)
		}
		if _, err := fs.ReadDir(filesystem, "."); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("listing the root fails with %v, want it not to exist", err)
		}
		if _, found, err := values.Get(filesystem, "volume"); found || err != nil {
			t.Errorf("volume gets found %v, %v, want no values", found, err)
		}
	})
	if logged := slices.Clone(h.errors); len(logged) != 0 {
		t.Errorf("the kernel logged %v", logged)
	}
}
