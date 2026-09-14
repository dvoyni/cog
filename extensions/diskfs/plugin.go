//go:build !js

package diskfs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// name is the plugin's name. Nothing orders against or configures the plugin by
// name: its Config goes to New.
const name kernel.PluginName = "diskfs"

// Config configures the desktop permanent filesystem.
type Config struct {
	// AppId names the directory under the user's data directory that writes land
	// in. It must be a single directory name. Empty means the executable's name
	// without its extension.
	AppId string
}

// New creates the diskfs plugin. It opens the permanent directory during
// Register and provides it as storage's PermanentFS Adapter.
func New(config Config) kernel.Plugin { return &plugin{config: config} }

type plugin struct{ config Config }

func (p *plugin) Name() kernel.PluginName { return name }

func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	appId, err := resolveAppId(p.config.AppId)
	if err != nil {
		return err
	}
	dir, err := permanentDir(appId)
	if err != nil {
		return err
	}
	permanent, err := openDiskFS(dir)
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[StoragePermanentFS](permanent)
	return nil
}

// ErrInvalidAppId reports an application id that is not a single directory name.
type ErrInvalidAppId struct{ AppId string }

func (e ErrInvalidAppId) Error() string {
	return fmt.Sprintf("diskfs: invalid app id %q", e.AppId)
}

// resolveAppId falls back to the executable's name and checks that the result
// names one directory, so the permanent directory cannot escape the data
// directory.
func resolveAppId(appId string) (string, error) {
	if appId == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("diskfs: resolve app id: %w", err)
		}
		appId = strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
	}
	if appId == "." || appId == ".." || filepath.Base(appId) != appId || filepath.VolumeName(appId) != "" {
		return "", ErrInvalidAppId{AppId: appId}
	}
	return appId, nil
}

func permanentDir(appId string) (string, error) {
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("diskfs: resolve user data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("diskfs: resolve user data directory: %w", err)
		}
	}
	return filepath.Join(base, appId), nil
}
