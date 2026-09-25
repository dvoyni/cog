//go:build js

package internal

import (
	"errors"
	"io/fs"
	"syscall/js"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// fakeLocalStorage installs a localStorage of getItem and setItem over a Go map
// on the global object for the length of the test, whatever the host provides,
// and returns the map. A restart keeps it, as a page reload keeps the browser's.
func fakeLocalStorage(t *testing.T) map[string]string {
	t.Helper()
	items := map[string]string{}
	getItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if value, ok := items[args[0].String()]; ok {
			return value
		}
		return js.Null()
	})
	setItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		items[args[0].String()] = args[1].String()
		return js.Undefined()
	})
	object := js.Global().Get("Object")
	fake := object.New()
	fake.Set("getItem", getItem)
	fake.Set("setItem", setItem)

	previous := object.Call("getOwnPropertyDescriptor", js.Global(), "localStorage")
	descriptor := object.New()
	descriptor.Set("value", fake)
	descriptor.Set("configurable", true)
	descriptor.Set("writable", true)
	object.Call("defineProperty", js.Global(), "localStorage", descriptor)
	t.Cleanup(func() {
		if previous.IsUndefined() {
			js.Global().Delete("localStorage")
		} else {
			object.Call("defineProperty", js.Global(), "localStorage", previous)
		}
		getItem.Release()
		setItem.Release()
	})
	return items
}

// Values set through storage's commands land under the app's localStorage key,
// and an engine started afterwards reads them back.
func TestValuesRoundTripAndSurviveARestart(t *testing.T) {
	items := fakeLocalStorage(t)
	config := Config{AppId: "cog-jsstorage-test"}

	first := start(t, config)
	if answer := first.kernel.ExecuteCommand[storage.AccessValuesCmd](storage.SetValue("volume", 0.25)); answer.Err != nil {
		t.Fatal(answer.Err)
	}
	volume := 1.0
	if response := first.kernel.ExecuteCommand[storage.AccessValuesCmd](storage.GetValue("volume", 1.0, &volume)); response.Err != nil || !response.Found || volume != 0.25 {
		t.Fatalf("GetValue = %v (found %v), %v; want 0.25 found", volume, response.Found, response.Err)
	}
	first.stop()

	if _, ok := items["cog.storage.cog-jsstorage-test"]; !ok {
		t.Fatalf("nothing written under cog.storage.cog-jsstorage-test; keys: %v", items)
	}

	second := start(t, config)
	defer second.stop()
	volume = 1.0
	response := second.kernel.ExecuteCommand[storage.AccessValuesCmd](storage.GetValue("volume", 1.0, &volume))
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	if !response.Found || volume != 0.25 {
		t.Fatalf("after a restart GetValue = %v (found %v), want 0.25 found", volume, response.Found)
	}
}

// A browser has no executable to name the app after, so an empty AppId is an
// error, and so is one that is not a single name.
func TestAnEmptyOrInvalidAppIdIsRejected(t *testing.T) {
	fakeLocalStorage(t)
	for _, appId := range []string{"", ".", "..", "a/b", `a\b`} {
		t.Run(appId, func(t *testing.T) {
			var reported []error
			kernel.New(map[kernel.PluginName]any{Name: Config{AppId: appId}}).Handler(func(err error) error {
				reported = append(reported, err)
				return err
			}).WithPlugins(storageplugin.New(), New())

			var invalid ErrInvalidAppId
			if !errors.As(errors.Join(reported...), &invalid) || invalid.AppId != appId {
				t.Fatalf("composition reported %v, want ErrInvalidAppId for %q", reported, appId)
			}
		})
	}
}

// A configuration value under jsstorage.Name that is not a jsstorage.Config
// fails Register rather than being read as an empty AppId.
func TestAConfigThatIsNotAConfigIsRejected(t *testing.T) {
	fakeLocalStorage(t)
	var reported []error
	kernel.New(map[kernel.PluginName]any{Name: "feuds"}).Handler(func(err error) error {
		reported = append(reported, err)
		return err
	}).WithPlugins(storageplugin.New(), New())

	var invalid ErrInvalidConfig
	if !errors.As(errors.Join(reported...), &invalid) || invalid.Got != "feuds" {
		t.Fatalf("composition reported %v, want ErrInvalidConfig for %q", reported, "feuds")
	}
}

func TestWebFSOperationsAndTraversal(t *testing.T) {
	fakeLocalStorage(t)
	permanent := &webFS{key: keyPrefix + "ops", storage: js.Global().Get("localStorage")}
	if err := permanent.MkdirAll("saves", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := permanent.WriteFile("saves/one.json", []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := permanent.WriteFile("saves/one.json", []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := permanent.Rename("saves/one.json", "saves/two.json"); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(permanent, "saves/two.json")
	if err != nil || string(data) != "newer" {
		t.Fatalf("ReadFile = %q, %v; want newer, nil", data, err)
	}
	if err := permanent.Remove("saves/two.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(permanent, "saves/two.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removed file stat error = %v, want fs.ErrNotExist", err)
	}
	if err := permanent.WriteFile("../escape", nil, 0o600); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("traversal error = %v, want fs.ErrInvalid", err)
	}
}

type running struct {
	kernel kernel.Executioner
	stop   func()
}

// start runs an engine of storage and jsstorage until stop, which waits for it
// to shut down, as a page unload would.
func start(t *testing.T, config Config) running {
	t.Helper()
	engine := kernel.New(map[kernel.PluginName]any{Name: config}).
		Handler(func(err error) error {
			t.Errorf("unexpected kernel error: %v", err)
			return err
		}).
		WithPlugins(storageplugin.New(), New())
	done := make(chan struct{})
	go func() {
		engine.Run()
		close(done)
	}()
	<-engine.Ready()
	return running{kernel: engine.Executioner(), stop: func() {
		engine.Quit()
		<-done
	}}
}
