package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// plugin publishes the id authority as a kernel resource, registers the one
// Command an app executes, ShrinkCmd, and the six Commands that read and write
// the world by Component name (censusCmd, entityCmd, queryCmd, spawnCmd,
// despawnCmd, updateCmd), registers the
// one Component the ECS owns, m.Transform, and subscribes the one System it
// owns, the general drainer (drainsystem.go). That is everything the ECS
// registers: every other Component is registered by the plugin that defines
// its Go type, and every other System is an ordinary subscription an app
// makes.
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

// Register publishes the authority, registers ShrinkCmd and the six by-name
// Commands, each of which holds that authority for write and nothing besides,
// registers the m.Transform Store, and subscribes the general drainer. One
// spelling per resource, everywhere:
// it is *ecs.Entities in every declaration, because resource cells are keyed
// by exact Go type and nothing normalises pointer-ness, so a second spelling
// would be a second cell that excludes nothing.
//
// The by-name Commands' price: a call waits for every running ECS System to
// release *ecs.Entities, and every System queued behind it waits until it
// returns. No frame's lock set widens, because they are Commands and not
// Systems, and a frame nobody calls into pays nothing for them. See read.go.
func (plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	registrar.InitResource(types.NewEntities(config.PrewarmEntities))
	registrar.HandleCommand[ecs.ShrinkCmd](types.ShrinkCommand)
	registrar.HandleCommand[censusCmd](types.CensusCommand)
	registrar.HandleCommand[entityCmd](types.EntityCommand)
	registrar.HandleCommand[queryCmd](types.QueryCommand)
	registrar.HandleCommand[spawnCmd](types.SpawnCommand)
	registrar.HandleCommand[despawnCmd](types.DespawnCommand)
	registrar.HandleCommand[updateCmd](types.UpdateCommand)
	// The six by-name Commands offered to an Agent. The Provider holds nothing
	// and subscribes nothing, and an app that composes no broker binds it to
	// nothing, so a game nobody debugs pays nothing for it.
	registrar.ProvideAdapter[ecs.McpProvider](mcp.Provider(provider{}))
	// Where an Entity stands is one Store every binding reads - scene draws at
	// it and sound is heard from it - so it belongs to neither. Two Components
	// describing one position would be two lock units the scheduler cannot
	// relate, and two Systems writing "the" position would run concurrently on
	// separate copies. It is registered unconditionally, because a game that
	// places nothing pays one empty Store.
	types.RegisterComponent[m.Transform](registrar, config.PrewarmEntities)
	// The general drainer, and the ECS's first dependency on the app slot. It is
	// subscribed unconditionally, with no flag to turn it off, because a queue
	// that is never drained is not a configuration: every deferring handle's
	// queue settles within the Update it was made in, or earlier, under an app's
	// own drain System. Last, so gameplay has run before it applies. The plugin
	// stays zero-dependency: it names app.UpdateEvent, which is an event type
	// and not a resource, so nothing here waits on the app plugin.
	registrar.Subscribe[ecs.DrainOnUpdate](
		types.ToHandler[app.UpdateEvent](registrar, drainSystem)).Last()
	return nil
}
