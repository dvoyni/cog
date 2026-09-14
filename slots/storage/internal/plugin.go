// Package internal is the storage plugin: New and the handlers behind storage's
// commands. Composition roots and tests reach New through storageplugin;
// everything else reaches storage through its root.
//
// The plugin requires exactly one storage.PermanentFS Adapter. A composition
// with none fails with kernel.ErrMissingAdapter.
package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/internal/types"
)

// plugin registers the FileSystem and Values resources and storage commands.
type plugin struct {
	// permanent is the bound PermanentFS Adapter. The resources read it through
	// its Get, which is valid from Start onwards.
	permanent kernel.RequiredAdapter[storage.PermanentFS]
}

// New creates a storage plugin. Its storage.Config arrives through kernel.New's
// config map under storage.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return storage.Name }

// Dependencies reports the plugins storage requires; it has none. The Adapter
// it requires is bound at composition and adds no dependency.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the PermanentFS Adapter, resolves the configuration and
// registers the storage resources and commands.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.permanent = registrar.RequireAdapter[storage.PermanentFSPort]()
	var cfg storage.Config
	if config != nil {
		var ok bool
		cfg, ok = config.(storage.Config)
		if !ok {
			return storage.ErrInvalidConfig{Got: config}
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

func resolveConfig(config storage.Config, permanent func() storage.PermanentFS) (storage.FileSystem, storage.Values, error) {
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

	return types.NewFileSystem(config.ReadMounts, permanent), types.NewValues(valuesPath), nil
}
