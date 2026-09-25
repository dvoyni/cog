//go:build !js

package internal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// isolateDataDir points the user's data directory at a temporary one on every
// platform, so a test never writes into the real one.
func isolateDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	return dir
}

// Values set through storage's commands land in the app's directory under the
// data directory, and an engine started afterwards reads them back.
func TestValuesRoundTripAndSurviveARestart(t *testing.T) {
	dataDir := isolateDataDir(t)
	config := Config{AppId: "cog-diskstorage-test"}

	first := start(t, config)
	if answer := first.kernel.ExecuteCommand[storage.AccessValuesCmd](storage.SetValue("volume", 0.25)); answer.Err != nil {
		t.Fatal(answer.Err)
	}
	volume := 1.0
	if response := first.kernel.ExecuteCommand[storage.AccessValuesCmd](storage.GetValue("volume", 1.0, &volume)); response.Err != nil || !response.Found || volume != 0.25 {
		t.Fatalf("GetValue = %v (found %v), %v; want 0.25 found", volume, response.Found, response.Err)
	}
	first.stop()

	if _, err := os.Stat(filepath.Join(dataDir, "cog-diskstorage-test", storage.DefaultValuesPath)); err != nil {
		t.Fatalf("values file not written under the app's data directory: %v", err)
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

func TestAnInvalidAppIdIsRejected(t *testing.T) {
	isolateDataDir(t)
	for _, appId := range []string{".", "..", "a/b", filepath.Join("a", "b"), filepath.Join(t.TempDir(), "abs")} {
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

// A configuration value under diskstorage.Name that is not a diskstorage.Config
// fails Register rather than falling back to the default.
func TestAConfigThatIsNotAConfigIsRejected(t *testing.T) {
	isolateDataDir(t)
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

// An empty AppId falls back to the executable's name.
func TestAnEmptyAppIdIsTheExecutableName(t *testing.T) {
	appId, err := resolveAppId("")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Base(executable[:len(executable)-len(filepath.Ext(executable))]); appId != want {
		t.Fatalf("app id = %q, want %q", appId, want)
	}
}

func TestDiskFSOperationsAndTraversal(t *testing.T) {
	permanent, err := openDiskFS(t.TempDir())
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

type running struct {
	kernel kernel.Executioner
	stop   func()
}

// start runs an engine of storage and diskstorage until stop, which waits for
// it to shut down, as a process exit would.
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
