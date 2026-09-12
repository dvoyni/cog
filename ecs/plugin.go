package ecs

import "github.com/dvoyni/cog/kernel"

// Name is the ecs plugin name and configuration key.
const Name kernel.PluginName = "ecs"

// plugin publishes the id authority as a kernel resource. It is everything the
// ECS registers: Components are registered by the plugins that define their Go
// types, and Systems are ordinary subscriptions.
type plugin struct{ entities *Entities }

// Plugin makes the world's id authority available to the engine as the resource
// every System holds for read and every structural change holds for write.
//
// The world handle is a plain Go value threaded through plugin constructors,
// and it has to be: both component registration and the handler builder need it
// at registration, where no handler is running and no resource value may be
// read. So the binding shape is fixed before WithPlugins is called and is
// visible in the app's composition root:
//
//	world := ecs.NewEntities(maxIDs)
//	kernel.New(config).WithPlugins(ecs.Plugin(world), physics.Plugin(world), game.Plugin(world))
//
// There is exactly one authority per Engine, which is what makes an Engine the
// boundary of one simulation: a second simulation is a second Engine, never a
// second Entities.
func Plugin(entities *Entities) kernel.Plugin {
	if entities == nil {
		panic("ecs: Plugin needs the Entities it publishes")
	}
	return &plugin{entities: entities}
}

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins ecs requires; it has none.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register publishes the authority. One spelling per resource, everywhere: it
// is *Entities in every declaration, because resource cells are keyed by exact
// Go type and nothing normalises pointer-ness, so a second spelling would be a
// second cell that excludes nothing.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(p.entities)
	return nil
}
