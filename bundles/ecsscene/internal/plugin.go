package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin is cog's ecs binding of model's drawing. Register ecs, model and gfx
// beside it, and not scene: an app runs ecsscene or scene, never both.
type plugin struct{}

// New makes the binding. Its Components and System belong to the ecs plugin's
// world, which it reaches at registration through its dependency on ecs:
//
//	kernel.New(config).WithPlugins(
//	    storageplugin.New(), diskstorageplugin.New(),
//	    gfxplugin.New(), modelplugin.New(),
//	    ecsplugin.New(), ecssceneplugin.New(), game.New())
//
// ecsscene has no configuration, so there is no Config.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return ecsscene.Name }

// Dependencies reports what the binding binds: ecs, whose Stores the Systems
// read, model, whose Lookup every model and mesh the binding names resolves
// against, and gfx, whose op queue the recording System draws into. The load
// System loads, so it also reads storage's filesystem and writes gfx's
// resource queue.
func (plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, model.Name, gfx.Name, storage.Name}
}

// The populations the Stores reserve for. They are hints, not caps: a Store
// grows by doubling past its reserve.
const (
	drawableReserve = 1024
	lightReserve    = 64
	cameraReserve   = 8
)

// Register declares every Component, the two scratches and the two Systems: the
// load System, ordered before the recording System, and the recording System.
// A Component is registered by the plugin that defines its Go type, which is
// what keeps cog's coupling check working on Component data: the types are
// declared in ecsscene's root, and this plugin, shipped in the same
// Bundle, registers them under ecsscene.Name. The m.Transform an Entity is
// drawn at is not among them: it is the ecs plugin's, and every binding reads
// the same Store.
func (plugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[ecsscene.Model](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Mesh](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Animation](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Params](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Material](registrar, drawableReserve)
	ecs.RegisterComponent[ecsscene.Light](registrar, lightReserve)
	ecs.RegisterComponent[ecsscene.Camera](registrar, cameraReserve)
	registrar.InitResource(newScratch())
	registrar.InitResource(newKeyScratch())
	registrar.Subscribe[ecsscene.LoadOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, load)).
		Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[ecsscene.RecordOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, record))
	return nil
}
