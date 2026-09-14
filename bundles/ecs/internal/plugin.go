package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// plugin publishes the id authority as a kernel resource. It is everything the
// ECS registers: Components are registered by the plugins that define their Go
// types, and Systems are ordinary subscriptions.
type plugin struct{}

// New makes the world's id authority available to the engine as the resource
// every System holds for read and every structural change holds for write.
//
// The authority is created here, from ecs.Config, and nowhere else. The plugins that
// register Components and Systems reach it at registration through
// kernel.Registrar.Dependency, which is why each of them declares a dependency
// on ecs.Name: that is what registers ecs, and so the authority, first.
//
//	kernel.New(config).WithPlugins(ecsplugin.New(), physics.New(), game.New())
//
// There is exactly one authority per Engine, which is what makes an Engine the
// boundary of one simulation: a second simulation is a second Engine, never a
// second Entities.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return ecs.Name }

// Dependencies reports the plugins ecs requires; it has none.
func (plugin) Dependencies() []kernel.PluginName { return nil }

// Register publishes the authority. One spelling per resource, everywhere: it
// is *ecs.Entities in every declaration, because resource cells are keyed by
// exact Go type and nothing normalises pointer-ness, so a second spelling would
// be a second cell that excludes nothing.
func (plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	registrar.InitResource(types.NewEntities(config.PrewarmEntities))
	return nil
}
