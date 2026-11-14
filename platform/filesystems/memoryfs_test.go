package filesystems_test

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
)

// bindMounts answers a bind of mounts as read mounts at their index's
// priority.
func bindMounts(mounts ...fs.FS) func(c *kernel.Configurator) {
	return func(c *kernel.Configurator) {
		for priority, mount := range mounts {
			c.BindSocket(platform.ReadMountSocket{FS: mount, Priority: priority})
		}
	}
}

// mustPut puts data into memory as name, failing the test if it cannot.
func mustPut(t *testing.T, memory *filesystems.MemoryFS, name, data string) {
	t.Helper()
	if err := memory.Put(name, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func TestPutsAndRemovesChangeWhatFSAnswersAtTheMountsPriorityUnderItsReadLock(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	below := fstest.MapFS{"readme": {Data: []byte("below")}, "beneath": {Data: []byte("beneath")}}
	above := fstest.MapFS{"shadowed": {Data: []byte("above")}}
	h := spawn(t, bindMounts(below, memory, above))
	h.reading(func(filesystem platform.FS, _ *platform.Values) {
		mustPut(t, memory, "readme", "put")
		mustPut(t, memory, "shadowed", "put")
		mustPut(t, memory, "images/embedded.png", "image")
		for name, want := range map[string]string{"readme": "put", "shadowed": "above", "images/embedded.png": "image", "beneath": "beneath"} {
			if data, err := fs.ReadFile(filesystem, name); err != nil || string(data) != want {
				t.Errorf("after the puts, %s reads %q, %v, want %q", name, data, err, want)
			}
		}
		if entries, err := fs.ReadDir(filesystem, "images"); err != nil || len(entries) != 1 || entries[0].Name() != "embedded.png" {
			t.Errorf("images lists %v, %v, want the put file", entries, err)
		}

		for _, name := range []string{"readme", "images/embedded.png"} {
			if err := memory.Remove(name); err != nil {
				t.Error(err)
			}
		}
		if data, err := fs.ReadFile(filesystem, "readme"); err != nil || string(data) != "below" {
			t.Errorf("after the remove, readme reads %q, %v, want the mount below's", data, err)
		}
		if _, err := fs.Stat(filesystem, "images"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("after its one file's remove, images stats %v, want it gone", err)
		}
	})
}

func TestTheMemoryFSPassesTestFS(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	files := map[string]string{"readme": "readme", "images/grass.png": "grass", "images/stone.png": "stone", "models/tree/bark.png": "bark", "empty": ""}
	for name, data := range files {
		mustPut(t, memory, name, data)
	}
	mustPut(t, memory, "removed/gone", "gone")
	if err := memory.Remove("removed/gone"); err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(memory, slices.Collect(maps.Keys(files))...); err != nil {
		t.Error(err)
	}
}

func TestStatAnswersTheSizeAndAModificationTimeStrictlyLaterAtEveryPut(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	before := time.Now()
	var last time.Time
	for put := range 10000 {
		name := fmt.Sprintf("file%d", put%3)
		data := make([]byte, put%7)
		if err := memory.Put(name, data); err != nil {
			t.Fatal(err)
		}
		info, err := memory.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != int64(len(data)) {
			t.Fatalf("put %d of %d bytes stats a size of %d", put, len(data), info.Size())
		}
		if !info.ModTime().After(last) {
			t.Fatalf("put %d stats modified at %v, not after the last put's %v", put, info.ModTime(), last)
		}
		last = info.ModTime()
	}
	if last.Before(before) {
		t.Errorf("the last put stats modified at %v, before the puts began at %v", last, before)
	}
}

func TestANameThatCannotBePutIsRefused(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	mustPut(t, memory, "file", "file")
	mustPut(t, memory, "directory/file", "file")
	for operation, err := range map[string]error{
		"putting ./slot":                 memory.Put("./slot", nil),
		"putting images//grass":          memory.Put("images//grass", nil),
		"putting the root":               memory.Put(".", nil),
		"putting over a directory":       memory.Put("directory", nil),
		"putting under a file":           memory.Put("file/beneath", nil),
		"removing a directory":           memory.Remove("directory"),
		"removing a name never put":      memory.Remove("missing"),
		"removing a name it cannot hold": memory.Remove("../file"),
	} {
		if err == nil {
			t.Errorf("%s succeeded", operation)
		}
	}
	for name, want := range map[string]string{"file": "file", "directory/file": "file"} {
		if data, err := fs.ReadFile(memory, name); err != nil || string(data) != want {
			t.Errorf("%s reads %q, %v, want %q as put", name, data, err, want)
		}
	}
}

func TestAPutKeepsItsOwnCopyAndAReadAnswersOne(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	data := []byte("put")
	if err := memory.Put("file", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	read, err := fs.ReadFile(memory, "file")
	if err != nil || string(read) != "put" {
		t.Fatalf("file reads %q, %v, after the caller changed its bytes, want %q", read, err, "put")
	}
	read[0] = 'X'
	if again, _ := fs.ReadFile(memory, "file"); string(again) != "put" {
		t.Errorf("file reads %q after a reader changed its bytes, want %q", again, "put")
	}
}

func TestPutsAndReadsFromManyGoroutines(t *testing.T) {
	memory := filesystems.NewMemoryFS()
	var group sync.WaitGroup
	for writer := range 4 {
		group.Go(func() {
			for put := range 200 {
				name := fmt.Sprintf("directory%d/file%d", writer, put%5)
				if err := memory.Put(name, []byte(name)); err != nil {
					t.Error(err)
					return
				}
				if put%3 == 0 {
					memory.Remove(name)
				}
			}
		})
	}
	for range 4 {
		group.Go(func() {
			for range 200 {
				fs.WalkDir(memory, ".", func(name string, entry fs.DirEntry, err error) error {
					if err == nil && !entry.IsDir() {
						fs.ReadFile(memory, name)
					}
					return nil
				})
			}
		})
	}
	group.Wait()
}
