package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// plugin is cog's ecs↔scene binding. Register ecs and scene beside it.
type plugin struct{}

// New makes the binding. Its Components and System belong to the ecs plugin's
// world, which it reaches at registration through its dependency on ecs:
//
//	kernel.New(config).WithPlugins(
//	    storageplugin.New(), diskfsplugin.New(),
//	    gfximpl.New(), sceneplugin.New(),
//	    ecsplugin.New(), ecssceneplugin.New(), game.New())
//
// ecsscene has no configuration, so there is no Config.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return ecsscene.Name }

// Dependencies reports both halves of the binding: the System reads the ECS's
// Stores and writes scene's queue.
func (plugin) Dependencies() []kernel.PluginName {
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
// what keeps cog's coupling check working on Component data: the types are
// declared in ecsscene's root, and this plugin, shipped in the same
// Bundle, registers them under ecsscene.Name.
func (plugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[ecsscene.Transform](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Model](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Mesh](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Animation](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Params](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Material](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Light](registrar, lightReserve)
	ecs.RegisterComponent[ecsscene.Camera](registrar, cameraReserve)
	registrar.InitResource(newScratch())
	registrar.Subscribe[ecsscene.RecordOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, record))
	return nil
}
