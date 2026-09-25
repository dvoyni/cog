package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
)

// plugin is cog's ecs-to-sound binding. Register ecs and sound beside it.
type plugin struct{}

// New makes the binding. Its Components and System belong to the ecs plugin's
// world, which it reaches at registration through its dependency on ecs:
//
//	kernel.New(config).WithPlugins(
//	    storageplugin.New(), diskstorageplugin.New(),
//	    soundplugin.New(), otosoundplugin.New(),
//	    ecsplugin.New(), ecsaudioplugin.New(), game.New())
//
// ecsaudio has no configuration, so there is no Config.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return Name }

// Dependencies reports both halves of the binding: the System reads the ECS's
// Stores and writes sound's queue.
func (plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, sound.Name}
}

// The populations the Stores reserve for. They are hints, not caps: a Store
// grows by doubling past its reserve. Emitters outnumber the Entities that are
// placed for audio alone, and there is one Listener.
const (
	emitterReserve  = 256
	listenerReserve = 4
)

// Register declares the two Components, the plugin-owned correspondence and
// the one System. The m.Transform they are placed by is the ecs plugin's. A
// Component is registered by the plugin that defines its Go type, which is what
// keeps cog's coupling check working on Component data: the types are declared
// in this package, aliased by ecsaudio's root, and registered here under
// ecsaudio.Name.
func (plugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Emitter](registrar, emitterReserve)
	ecs.RegisterComponent[Listener](registrar, listenerReserve)
	registrar.InitResource(newTable())
	registrar.Subscribe[RecordOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, recordSystem))
	return nil
}
