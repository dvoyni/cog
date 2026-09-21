// Package internal is the storage plugin: New and the handlers behind storage's
// commands. Composition roots and tests reach New through storageplugin;
// everything else reaches storage through its root.
//
// The plugin requires exactly one storage.PermanentFS Adapter. A composition
// with none fails with kernel.ErrMissingAdapter. It collects every
// storage.ReadMount contributed through storage.ReadMountPort and installs them
// at Start.
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
	// mounts are the contributed read mounts, installed by Start.
	mounts kernel.CollectedAdapters[storage.ReadMount]
}

// New creates a storage plugin. Its storage.Config arrives through kernel.New's
// config map under storage.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return storage.Name }

// Dependencies reports the plugins storage requires; it has none. The Adapter
// it requires is bound at composition and adds no dependency.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the PermanentFS Adapter, collects the read mounts, resolves
// the configuration and registers the storage resources and commands.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.permanent = registrar.RequireAdapter[storage.PermanentFSPort]()
	p.mounts = registrar.CollectAdapters[storage.ReadMountPort]()
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

// Start installs the contributed read mounts. Adapters bind only after every
// Register, so this is the first point storage can see them, and plugins start
// in dependency order, so every plugin that depends on storage finds them in
// place. An id contributed twice fails before anything is mounted; each mount
// then goes through SetMountCmd, which refuses an invalid or reserved one.
func (p *plugin) Start(k kernel.Executioner) error {
	contributed := p.mounts.Get()
	contributors := make(map[storage.MountId][]kernel.PluginName, len(contributed))
	for _, contribution := range contributed {
		contributors[contribution.Adapter.Id] = append(contributors[contribution.Adapter.Id], contribution.Plugin)
	}
	for _, contribution := range contributed {
		if plugins := contributors[contribution.Adapter.Id]; len(plugins) > 1 {
			return storage.ErrDuplicateMount{Id: contribution.Adapter.Id, Plugins: plugins}
		}
	}
	for _, contribution := range contributed {
		if err := k.ExecuteCommand[storage.SetMountCmd](storage.SetMountRequest{Mount: contribution.Adapter}).Err; err != nil {
			return err
		}
	}
	return nil
}

func resolveConfig(config storage.Config, permanent func() storage.PermanentFS) (storage.FileSystem, storage.Values, error) {
	valuesPath := config.ValuesPath
	if valuesPath == "" {
		valuesPath = storage.DefaultValuesPath
	}
	if !fs.ValidPath(valuesPath) || valuesPath == "." {
		return storage.FileSystem{}, storage.Values{}, storage.ErrInvalidValuesPath{Path: valuesPath}
	}

	return types.NewFileSystem(nil, permanent), types.NewValues(valuesPath), nil
}
