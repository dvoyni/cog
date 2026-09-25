//go:build !js

package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// New creates the diskstorage plugin. Its diskstorage.Config arrives through
// kernel.New's config map under diskstorage.Name. It opens the permanent
// directory during Register and provides it as storage's PermanentFS Adapter.
func New() kernel.Plugin { return &plugin{} }

type plugin struct{}

func (p *plugin) Name() kernel.PluginName { return Name }

func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	var cfg Config
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}
	appId, err := resolveAppId(cfg.AppId)
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

// resolveAppId falls back to the executable's name and checks that the result
// names one directory, so the permanent directory cannot escape the data
// directory.
func resolveAppId(appId string) (string, error) {
	if appId == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("diskstorage: resolve app id: %w", err)
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
				return "", fmt.Errorf("diskstorage: resolve user data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("diskstorage: resolve user data directory: %w", err)
		}
	}
	return filepath.Join(base, appId), nil
}
