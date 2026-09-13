package ecs

import (
	"github.com/dvoyni/cog/bundles/ecs/internal"
	"github.com/dvoyni/cog/kernel"
)

// authority stands in for ecsimpl in this package's own tests. They cannot
// compose ecsimpl.New(), because ecsimpl imports this package, and they need
// this package's unexported state to make the claims they make. It registers
// exactly what ecsimpl's plugin registers - the authority, under Name - so every
// composition here is the one an app builds; ecsimpl's own tests compose the
// real plugin.
type authority struct {
	// ids is how many Entities the authority reserves room for.
	ids uint32
}

func (authority) Name() kernel.PluginName { return Name }

func (authority) Dependencies() []kernel.PluginName { return nil }

func (a authority) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(internal.NewEntities(a.ids))
	return nil
}
