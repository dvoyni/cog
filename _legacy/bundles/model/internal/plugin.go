package internal

import (
	"math"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin registers model's one resource and mounts the bundled shader. It runs
// nothing of its own: a renderer's flush holds the Lookup for writing, loads
// through it, and drives its bake and release queues at the frame boundary.
type plugin struct{}

// New returns the model plugin. It declares *model.Lookup, sized from
// model.Config under model.Name, and contributes the bundled shader's sources
// as a storage read mount, so it requires storage.
//
// Register it before any renderer that draws from it.
func New() kernel.Plugin { return &plugin{} }

func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies is storage alone, which hosts the bundled shader's mount. The
// Lookup holds no handle of its own: the filesystem and resource queue a load
// needs arrive from the handler that holds them.
func (p *plugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{storage.Name} }

func (p *plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	registrar.InitResource(NewSizedLookup(config))
	// storage installs the bundled shader mount at its Start, ahead of every
	// plugin that depends on it, so the shader is in place for the first frame
	// of any renderer drawing with it.
	registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
		Id: shaderMountID, Priority: math.MaxInt, FS: shaderFS,
	})
	return nil
}
