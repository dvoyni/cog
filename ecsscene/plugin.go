package ecsscene

import (
	"fmt"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/scene"
)

// Plugin is cog's ecs↔scene binding. Register ecs and scene before it.
type Plugin struct {
	world *ecs.Entities
}

// New makes the binding for one world. The world handle is threaded through the
// constructor because Component registration needs it at registration, where no
// handler is running and no resource value may be read:
//
//	world := ecs.NewEntities(4096)
//	kernel.New(config).WithPlugins(
//	    storage.New(), gfx.New(), scene.New(),
//	    ecs.Plugin(world), ecsscene.New(world), game.New(world))
func New(world *ecs.Entities) *Plugin {
	if world == nil {
		panic("ecsscene: New needs the Entities its Components belong to")
	}
	return &Plugin{world: world}
}

func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports both halves of the binding: the System reads the ECS's
// Stores and writes scene's queue.
func (p *Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name}
}

// The populations the Stores reserve for. They are hints, not caps: a Store
// grows by doubling past its reserve.
const (
	drawableReserve = 1024
	lightReserve    = 64
	cameraReserve   = 8
)

// Register declares every Component, the recording scratch and the one System.
// A Component is registered by the plugin that defines its Go type, which is
// what keeps cog's coupling check working on Component data.
func (p *Plugin) Register(registrar *kernel.Registrar, value any) error {
	if value != nil {
		if _, ok := value.(Config); !ok {
			return fmt.Errorf("ecsscene: invalid config %T", value)
		}
	}
	ecs.RegisterComponent[Transform](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Model](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Mesh](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Animation](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Params](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Material](registrar, p.world, drawableReserve)
	ecs.RegisterComponent[Light](registrar, p.world, lightReserve)
	ecs.RegisterComponent[Camera](registrar, p.world, cameraReserve)
	registrar.InitResource(newScratch())
	registrar.Subscribe[UpdateEventHandler](ecs.ToHandler[app.UpdateEvent](p.world, record))
	return nil
}
