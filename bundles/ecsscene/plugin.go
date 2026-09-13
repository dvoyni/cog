package ecsscene

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Plugin is cog's ecs↔scene binding. Register ecs and scene beside it.
type Plugin struct{}

// New makes the binding. Its Components and System belong to the ecs plugin's
// world, which it reaches at registration through its dependency on ecs:
//
//	kernel.New(config).WithPlugins(
//	    storage.New(), gfximpl.New(), scene.New(),
//	    ecs.Plugin(), ecsscene.New(), game.New())
func New() *Plugin { return &Plugin{} }

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
	ecs.RegisterComponent[Transform](registrar, drawableReserve)
	ecs.RegisterComponent[Model](registrar, drawableReserve)
	ecs.RegisterComponent[Mesh](registrar, drawableReserve)
	ecs.RegisterComponent[Animation](registrar, drawableReserve)
	ecs.RegisterComponent[Params](registrar, drawableReserve)
	ecs.RegisterComponent[Material](registrar, drawableReserve)
	ecs.RegisterComponent[Light](registrar, lightReserve)
	ecs.RegisterComponent[Camera](registrar, cameraReserve)
	registrar.InitResource(newScratch())
	registrar.Subscribe[UpdateEventHandler](ecs.ToHandler[app.UpdateEvent](registrar, record))
	return nil
}
