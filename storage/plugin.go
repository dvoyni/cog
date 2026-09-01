package storage

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// Name is the storage plugin name and configuration key.
const Name kernel.PluginName = "storage"

// Plugin registers ReadFS and WriteFS resources and storage commands.
type Plugin struct{}

// New creates a storage plugin.
func New() *Plugin { return &Plugin{} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins storage requires; it has none.
func (p *Plugin) Dependencies() []kernel.PluginName { return nil }

// Register resolves defaults and registers the storage resources.
func (p *Plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := DefaultConfig("")
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}

	readFS, writeFS, err := resolveConfig(cfg)
	if err != nil {
		return err
	}
	registrar.InitResource(readFS)
	registrar.InitResource(writeFS)
	registerCommands(registrar)
	return nil
}

func resolveConfig(config Config) (ReadFS, WriteFS, error) {
	mounts := make([]ReadMount, 0, len(config.ReadMounts)+1)
	for _, mount := range config.ReadMounts {
		if mount.Id == "" || mount.FS == nil {
			return ReadFS{}, nil, ErrInvalidMount{Id: mount.Id}
		}
		mounts = append(mounts, mount)
	}

	if !hasMount(mounts, ExecutableMount) {
		mount, available, err := defaultReadMount()
		if err != nil {
			return ReadFS{}, nil, err
		}
		if available {
			mounts = append(mounts, mount)
		}
	}

	writeFS := config.WriteFS
	if writeFS == nil {
		appId := config.AppId
		if appId == "" {
			executable, err := os.Executable()
			if err != nil {
				return ReadFS{}, nil, fmt.Errorf("storage: resolve app id: %w", err)
			}
			appId = strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
		}
		if appId == "." || appId == ".." || filepath.Base(appId) != appId || filepath.VolumeName(appId) != "" {
			return ReadFS{}, nil, ErrInvalidAppId{AppId: appId}
		}
		var err error
		writeFS, err = defaultWriteFS(appId)
		if err != nil {
			return ReadFS{}, nil, err
		}
	}

	if !hasMount(mounts, PermanentMount) {
		mounts = append(mounts, ReadMount{Id: PermanentMount, Priority: math.MaxInt, FS: writeFS})
	}
	return newReadFS(mounts), writeFS, nil
}

func hasMount(mounts []ReadMount, id MountId) bool {
	for _, mount := range mounts {
		if mount.Id == id {
			return true
		}
	}
	return false
}

var _ fs.FS = readFS{}
