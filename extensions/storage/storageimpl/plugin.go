// Package storageimpl is the storage plugin: New, its Config and the handlers
// behind storage's commands. Only composition roots and tests import it;
// everything else reaches storage through its contract root.
//
// The plugin requires exactly one storage.PermanentFS Adapter. A composition
// with none fails with kernel.ErrMissingAdapter.
package storageimpl

import (
	"io/fs"

	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/extensions/storage/internal"
	"github.com/dvoyni/cog/kernel"
)

// Plugin registers the FileSystem and Values resources and storage commands.
type Plugin struct {
	// permanent is the bound PermanentFS Adapter. The resources read it through
	// its Get, which is valid from Start onwards.
	permanent kernel.RequiredAdapter[storage.PermanentFS]
}

// New creates a storage plugin. Its Config arrives through kernel.New's config
// map under storage.Name.
func New() *Plugin { return &Plugin{} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return storage.Name }

// Dependencies reports the plugins storage requires; it has none. The Adapter
// it requires is bound at composition and adds no dependency.
func (p *Plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the PermanentFS Adapter, resolves the configuration and
// registers the storage resources and commands.
func (p *Plugin) Register(registrar *kernel.Registrar, config any) error {
	p.permanent = registrar.RequireAdapter[storage.PermanentFS]()
	cfg := DefaultConfig()
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}

	filesystem, values, err := resolveConfig(cfg, p.permanent.Get)
	if err != nil {
		return err
	}
	registrar.InitResource(filesystem)
	registrar.InitResource(values)
	registerCommands(registrar)
	return nil
}

func resolveConfig(config Config, permanent func() storage.PermanentFS) (storage.FileSystem, storage.Values, error) {
	for _, mount := range config.ReadMounts {
		if mount.Id == "" || mount.FS == nil {
			return storage.FileSystem{}, storage.Values{}, storage.ErrInvalidMount{Id: mount.Id}
		}
		if mount.Id == storage.PermanentMount {
			return storage.FileSystem{}, storage.Values{}, storage.ErrReservedMount{Id: mount.Id}
		}
	}

	valuesPath := config.ValuesPath
	if valuesPath == "" {
		valuesPath = storage.DefaultValuesPath
	}
	if !fs.ValidPath(valuesPath) || valuesPath == "." {
		return storage.FileSystem{}, storage.Values{}, storage.ErrInvalidValuesPath{Path: valuesPath}
	}

	return internal.NewFileSystem(config.ReadMounts, permanent), internal.NewValues(valuesPath), nil
}
