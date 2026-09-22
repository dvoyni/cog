package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// plugin registers model's one resource. It runs nothing of its own: a
// renderer's flush holds the Lookup for writing, loads through it, and drives
// its bake and release queues at the frame boundary.
type plugin struct{}

// New returns the model plugin. It declares *model.Lookup, sized from
// model.Config under model.Name, and requires nothing.
//
// Register it before any renderer that draws from it.
func New() kernel.Plugin { return &plugin{} }

func (p *plugin) Name() kernel.PluginName { return model.Name }

// Dependencies is empty: the Lookup holds no handle of its own, and the
// filesystem and resource queue a load needs arrive from the handler that
// holds them.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	registrar.InitResource(types.NewSizedLookup(config))
	return nil
}
