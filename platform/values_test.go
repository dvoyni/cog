package platform_test

import (
	"encoding/json"
	"io/fs"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

func TestValuesAreReadOnceThroughTheOverlayAndFlushedToTheWritableLayer(t *testing.T) {
	f := newFake()
	defaults := fstest.MapFS{"config.json": {Data: []byte(`{"volume": 0.8, "name": "default"}`)}}
	f.mounts = []platform.ReadMountSocket{{FS: defaults}}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if value, found, err := values.Get(filesystem, "volume"); err != nil || !found || string(value) != "0.8" {
			t.Errorf("volume gets %s, %v, %v, want the read mount's 0.8", value, found, err)
		}
	})
	defaults["config.json"].Data = []byte(`{"volume": 0.1}`)
	h.writing(func(filesystem platform.WriteFS, values *platform.Values) {
		for _, err := range []error{
			values.Set(filesystem, "volume", json.RawMessage(`0.5`)),
			values.Delete(filesystem, "name"),
			values.Delete(filesystem, "missing"),
		} {
			if err != nil {
				t.Error(err)
				return
			}
		}
		if _, found := f.writable.MapFS["config.json"]; found {
			t.Error("a change reached the file before a flush")
		}
		if err := values.Flush(filesystem); err != nil {
			t.Error(err)
		}
	})
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if value, _, _ := values.Get(filesystem, "volume"); string(value) != "0.5" {
			t.Errorf("volume gets %s after a set, want 0.5, the file read once", value)
		}
		if _, found, _ := values.Get(filesystem, "name"); found {
			t.Error("a deleted key is found")
		}
	})
	var flushed map[string]json.RawMessage
	if err := json.Unmarshal(f.writable.MapFS["config.json"].Data, &flushed); err != nil || len(flushed) != 1 || string(flushed["volume"]) != "0.5" {
		t.Errorf("the writable layer's config.json holds %v, %v, want volume 0.5 alone", flushed, err)
	}
}

func TestValuesFileIsConfigurable(t *testing.T) {
	f := newFake()
	f.writable.MapFS["settings.json"] = &fstest.MapFile{Data: []byte(`{"name": "ann"}`)}
	h := spawnConfigured(t, f, platform.Config{ValuesFile: "settings.json"})
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if value, found, err := values.Get(filesystem, "name"); err != nil || !found || string(value) != `"ann"` {
			t.Errorf("name gets %s, %v, %v from settings.json", value, found, err)
		}
	})
}

func TestACorruptValuesFileIsAnErrorAndNotJSONIsRefused(t *testing.T) {
	f := newFake()
	f.writable.MapFS["config.json"] = &fstest.MapFile{Data: []byte(`{"volume":`)}
	h := spawn(t, f)
	h.reading(func(filesystem platform.FS, values *platform.Values) {
		if _, _, err := values.Get(filesystem, "volume"); err == nil || !strings.Contains(err.Error(), "corrupt") {
			t.Errorf("a corrupt file gets %v, want an error saying so", err)
		}
	})
	f = newFake()
	h = spawn(t, f)
	h.writing(func(filesystem platform.WriteFS, values *platform.Values) {
		if err := values.Set(filesystem, "volume", json.RawMessage(`not json`)); err == nil {
			t.Error("a value that is not JSON is set")
		}
	})
}

func TestAClosedWindowFlushesTheValues(t *testing.T) {
	f := newFake()
	h := spawn(t, f)
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.writing(func(filesystem platform.WriteFS, values *platform.Values) {
		if err := values.Set(filesystem, "volume", json.RawMessage(`0.5`)); err != nil {
			t.Error(err)
		}
	})
	h.change(func(manager *platform.WindowManager) { manager.Close(window) })
	h.pump()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var flushed bool
		h.reading(func(filesystem platform.FS, values *platform.Values) {
			_, err := filesystem.Open("config.json")
			flushed = err == nil
		})
		if flushed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("closing a window did not flush the values in 5s")
		}
		runtime.Gosched()
	}
}

func TestAHandlerOrderedBeforeTheValuesFlushIsFlushedWithIt(t *testing.T) {
	f := newFake()
	h := &harness{t: t, fake: f}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	closing := &secondInstaller{func(c *kernel.Configurator) {
		handlers, _ := c.Resource[platform.Handlers]()
		markClosed := c.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Write[platform.FS], values kernel.Write[*platform.Values]) kernel.NoResult {
			if err := values.Get().Set(platform.ElevateFileAccess(filesystem), "closed", json.RawMessage(`true`)); err != nil {
				t.Error(err)
			}
			return kernel.NoResult{}
		})
		markOnClose := c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
			return event, ex.Schedule(markClosed, kernel.NoArg{}, in).Handle()
		})
		c.OrderSubscriptions(markOnClose, handlers.FlushValues)
	}}
	id, err := h.supervisor.Spawn(nil, 2, f, platform.NewPlugin(), h, closing)
	if err != nil {
		t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.change(func(manager *platform.WindowManager) { manager.Close(window) })
	h.pump()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var flushed json.RawMessage
		h.reading(func(filesystem platform.FS, _ *platform.Values) {
			flushed, _ = fs.ReadFile(filesystem, "config.json")
		})
		if flushed != nil {
			var written map[string]json.RawMessage
			if json.Unmarshal(flushed, &written) != nil || string(written["closed"]) != "true" {
				t.Errorf("the flush wrote %s, want the value set before it", flushed)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("closing a window did not flush the value set before the flush in 5s")
		}
		runtime.Gosched()
	}
}
