package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// Name is the storage plugin name and configuration key.
const Name kernel.PluginName = "storage"

// Plugin registers the FileSystem and Values resources and storage commands.
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

	filesystem, values, err := resolveConfig(cfg)
	if err != nil {
		return err
	}
	registrar.InitResource(filesystem)
	registrar.InitResource(values)
	registerCommands(registrar)
	return nil
}

func resolveConfig(config Config) (FileSystem, Values, error) {
	mounts := make([]ReadMount, 0, len(config.ReadMounts)+1)
	for _, mount := range config.ReadMounts {
		if mount.Id == "" || mount.FS == nil {
			return FileSystem{}, Values{}, ErrInvalidMount{Id: mount.Id}
		}
		if mount.Id == PermanentMount {
			return FileSystem{}, Values{}, ErrReservedMount{Id: mount.Id}
		}
		mounts = append(mounts, mount)
	}

	if !hasMount(mounts, ExecutableMount) {
		mount, available, err := defaultReadMount()
		if err != nil {
			return FileSystem{}, Values{}, err
		}
		if available {
			mounts = append(mounts, mount)
		}
	}

	valuesPath := config.ValuesPath
	if valuesPath == "" {
		valuesPath = DefaultValuesPath
	}
	if !fs.ValidPath(valuesPath) || valuesPath == "." {
		return FileSystem{}, Values{}, ErrInvalidValuesPath{Path: valuesPath}
	}

	permanent := config.PermanentFS
	if permanent == nil {
		appId := config.AppId
		if appId == "" {
			executable, err := os.Executable()
			if err != nil {
				return FileSystem{}, Values{}, fmt.Errorf("storage: resolve app id: %w", err)
			}
			appId = strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
		}
		if appId == "." || appId == ".." || filepath.Base(appId) != appId || filepath.VolumeName(appId) != "" {
			return FileSystem{}, Values{}, ErrInvalidAppId{AppId: appId}
		}
		var err error
		permanent, err = defaultPermanentFS(appId)
		if err != nil {
			return FileSystem{}, Values{}, err
		}
	}

	return newFileSystem(mounts, permanent), Values{path: valuesPath}, nil
}

func hasMount(mounts []ReadMount, id MountId) bool {
	for _, mount := range mounts {
		if mount.Id == id {
			return true
		}
	}
	return false
}

var _ fs.FS = fileSystem{}
