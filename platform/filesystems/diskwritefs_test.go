//go:build !js

package filesystems_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
)

// makeDirectory makes a temporary directory holding files, each name a
// slash-separated path, and answers its path.
func makeDirectory(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, data := range files {
		file := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

// newDiskWriteFS opens directory as a DiskWriteFS, closed when the test
// ends.
func newDiskWriteFS(t *testing.T, directory string) *filesystems.DiskWriteFS {
	t.Helper()
	writable, err := filesystems.NewDiskWriteFS(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writable.Close() })
	return writable
}

// openMount opens directory as a read mount's FS, closed when the test
// ends.
func openMount(t *testing.T, directory string) fs.FS {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root.FS()
}

// bindDisk answers a bind of writable as the writable filesystem and of
// mounts as read mounts at their index's priority.
func bindDisk(writable *filesystems.DiskWriteFS, mounts ...fs.FS) func(c *kernel.Configurator) {
	return func(c *kernel.Configurator) {
		c.BindSocket[platform.WriteFSSocket](writable)
		for priority, mount := range mounts {
			c.BindSocket(platform.ReadMountSocket{FS: mount, Priority: priority})
		}
	}
}

func TestTheDiskWriteFSAndDirectoryMountsPassTestFSAsOneOverlay(t *testing.T) {
	h := spawn(t, bindDisk(
		newDiskWriteFS(t, makeDirectory(t, map[string]string{"saves/slot1": "save"})),
		openMount(t, makeDirectory(t, map[string]string{"textures/grass.png": "grass", "readme": "low"})),
		openMount(t, makeDirectory(t, map[string]string{"textures/stone.png": "stone", "readme": "high"})),
	))
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		if err := fstest.TestFS(filesystem, "saves/slot1", "textures/grass.png", "textures/stone.png", "readme"); err != nil {
			t.Error(err)
		}
		if data, err := fs.ReadFile(filesystem, "readme"); err != nil || string(data) != "high" {
			t.Errorf("readme reads %q, %v, want the higher mount's", data, err)
		}
	})
}

func TestAMissingDirectoryIsCreatedWithItsParents(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "user", "game")
	h := spawn(t, bindDisk(newDiskWriteFS(t, directory)))
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		t.Fatalf("the directory stats %v, %v, want a directory", info, err)
	}
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		if err := filesystem.WriteFile("saves", []byte("save"), 0o644); err != nil {
			t.Error(err)
		}
	})
	if data, err := os.ReadFile(filepath.Join(directory, "saves")); err != nil || string(data) != "save" {
		t.Errorf("the created directory holds %q, %v, want the write", data, err)
	}
	file := filepath.Join(makeDirectory(t, map[string]string{"file": "a file"}), "file")
	if writable, err := filesystems.NewDiskWriteFS(filepath.Join(file, "game")); err == nil {
		writable.Close()
		t.Error("a directory under a file opened")
	}
}

func TestWritesLandInTheDirectoryAndReadBack(t *testing.T) {
	directory := t.TempDir()
	mount := makeDirectory(t, map[string]string{"readme": "mounted"})
	h := spawn(t, bindDisk(newDiskWriteFS(t, directory), openMount(t, mount)))
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		for _, err := range []error{
			filesystem.MkdirAll("saves/old", 0o755),
			filesystem.WriteFile("saves/slot1", []byte("one"), 0o644),
			filesystem.AppendFile("saves/slot1", []byte("+two"), 0o644),
			filesystem.AppendFile("saves/log", []byte("started"), 0o600),
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
	for name, want := range map[string]string{"saves/slot1": "one+two", "saves/slot2": "two", "saves/log": "started", "readme": "written"} {
		if data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(name))); err != nil || string(data) != want {
			t.Errorf("the directory's %s holds %q, %v, want %q", name, data, err, want)
		}
	}
	if info, err := os.Stat(filepath.Join(directory, "saves", "log")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("saves/log, created by an append, stats %v, %v, want mode 0600", info, err)
	}
	for _, name := range []string{"saves/old", "saves/temporary"} {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name))); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s stats %v, want it gone", name, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(mount, "readme")); string(data) != "mounted" {
		t.Errorf("a write reached the read mount's readme, now %q", data)
	}
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		for name, want := range map[string]string{"saves/slot1": "one+two", "saves/slot2": "two", "readme": "written"} {
			if data, _ := fs.ReadFile(filesystem, name); string(data) != want {
				t.Errorf("%s reads back %q, want %q", name, data, want)
			}
		}
	})
}

func TestANameOutsideTheDirectoryIsRefused(t *testing.T) {
	outside := makeDirectory(t, map[string]string{"secret": "outside"})
	directory := makeDirectory(t, map[string]string{"kept": "kept"})
	if err := os.Symlink(outside, filepath.Join(directory, "outside")); err != nil {
		t.Skipf("symlinks cannot be made here: %v", err)
	}
	h := spawn(t, bindDisk(newDiskWriteFS(t, directory)))
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		for operation, err := range map[string]error{
			"writing through a link out":   filesystem.WriteFile("outside/written", []byte("escaped"), 0o644),
			"appending through a link out": filesystem.AppendFile("outside/secret", []byte("escaped"), 0o644),
			"making a folder through it":   filesystem.MkdirAll("outside/folder", 0o755),
			"renaming out through it":      filesystem.Rename("outside/secret", "stolen"),
			"removing through it":          filesystem.Remove("outside/secret"),
			"writing to a parent":          filesystem.WriteFile("../written", []byte("escaped"), 0o644),
			"renaming to a parent":         filesystem.Rename("kept", "../stolen"),
		} {
			if err == nil {
				t.Errorf("%s succeeded", operation)
			}
		}
		if data, err := fs.ReadFile(filesystem, "outside/secret"); err == nil {
			t.Errorf("outside/secret, through a link out, reads %q", data)
		}
	})
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Errorf("the directory outside holds %v, %v, want its secret alone", entries, err)
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "secret")); string(data) != "outside" {
		t.Errorf("the secret outside now holds %q", data)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(directory), "written")); err == nil {
		t.Error("a write landed in the directory's parent")
	}
}

func TestANameFSWouldNotOpenIsNotWritten(t *testing.T) {
	directory := makeDirectory(t, map[string]string{"kept": "kept"})
	h := spawn(t, bindDisk(newDiskWriteFS(t, directory)))
	h.writing(func(filesystem platform.WriteFS, _ *platform.Values) {
		for operation, err := range map[string]error{
			"writing ./slot":          filesystem.WriteFile("./slot", []byte("save"), 0o644),
			"appending to saves//log": filesystem.AppendFile("saves//log", []byte("save"), 0o644),
			"making saves/./old":      filesystem.MkdirAll("saves/./old", 0o755),
			"removing kept/":          filesystem.Remove("kept/"),
			"renaming kept to /kept":  filesystem.Rename("kept", "/kept"),
			"renaming ./kept":         filesystem.Rename("./kept", "moved"),
		} {
			if !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("%s fails with %v, want an invalid name", operation, err)
			}
		}
	})
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "kept" {
		t.Errorf("the directory holds %v, %v, want kept alone", entries, err)
	}
}

func TestValuesFlushToTheDiskAndReadBack(t *testing.T) {
	directory := t.TempDir()
	defaults := makeDirectory(t, map[string]string{"config.json": `{"volume": 0.8}`})
	h := spawn(t, bindDisk(newDiskWriteFS(t, directory), openMount(t, defaults)))
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
	var flushed map[string]json.RawMessage
	data, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil || json.Unmarshal(data, &flushed) != nil || string(flushed["volume"]) != "0.5" {
		t.Fatalf("the directory's config.json holds %s, %v, want volume 0.5", data, err)
	}

	h.respawn(bindDisk(newDiskWriteFS(t, directory), openMount(t, defaults)))
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if value, _, err := values.Get(filesystem, "volume"); err != nil || string(value) != "0.5" {
			t.Errorf("a new kernel gets volume %s, %v, want the flushed 0.5", value, err)
		}
	})
}

func TestAClosedDiskWriteFSFailsEveryCall(t *testing.T) {
	directory := makeDirectory(t, map[string]string{"kept": "kept"})
	writable, err := filesystems.NewDiskWriteFS(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(writable, "kept"); err == nil {
		t.Error("reading after Close succeeded")
	}
	if err := writable.WriteFile("written", []byte("late"), 0o644); err == nil {
		t.Error("writing after Close succeeded")
	}
	if _, err := os.Stat(filepath.Join(directory, "written")); err == nil {
		t.Error("a write after Close landed")
	}
}
