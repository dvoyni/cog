package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// The populations the Stores reserve for. They are hints, not caps: a Store
// grows by doubling past its reserve.
const (
	drawableReserve = 1024
	lightReserve    = 64
	cameraReserve   = 8
	debugReserve    = 64
)

// The debug shapes' Systems after the first, which is scene.DebugOnUpdate.
// Each shape's change System runs before its bake System, so a shape whose
// Mesh the change System took away is baked again in the same tick.
type (
	debugBoxBakeOnUpdate       kernel.Subscription[app.UpdateEvent]
	debugSphereChangeOnUpdate  kernel.Subscription[app.UpdateEvent]
	debugSphereBakeOnUpdate    kernel.Subscription[app.UpdateEvent]
	debugPlaneChangeOnUpdate   kernel.Subscription[app.UpdateEvent]
	debugPlaneBakeOnUpdate     kernel.Subscription[app.UpdateEvent]
	debugLineChangeOnUpdate    kernel.Subscription[app.UpdateEvent]
	debugLineBakeOnUpdate      kernel.Subscription[app.UpdateEvent]
	debugWireBoxChangeOnUpdate kernel.Subscription[app.UpdateEvent]
	debugWireBoxBakeOnUpdate   kernel.Subscription[app.UpdateEvent]
)

// plugin is cog's ecs binding of model's drawing. Register ecs, model, gfx and
// storage beside it.
type plugin struct{}

// New makes the binding. Its Components and System belong to the ecs plugin's
// world, which it reaches at registration through its dependency on ecs:
//
//	kernel.New(config).WithPlugins(
//	    storageplugin.New(), diskstorageplugin.New(),
//	    gfxplugin.New(), modelplugin.New(),
//	    ecsplugin.New(), sceneplugin.New(), game.New())
//
// scene has no configuration, so there is no Config.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return Name }

// Dependencies reports what the binding binds: ecs, whose Stores the Systems
// read, model, whose Lookup every model and mesh the binding names resolves
// against, and gfx, whose op queue the recording System draws into. The load
// System loads, so it also reads storage's filesystem and writes gfx's
// resource queue.
func (plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, model.Name, gfx.Name, storage.Name}
}

// Register declares every Component, the scratches and the Systems: the load
// System, ordered before the recording System, the recording System, and
// through registerDebug the debug shapes' ten.
// A Component is registered by the plugin that defines its Go type, which is
// what keeps cog's coupling check working on Component data: the types are
// declared in this package, aliased by scene's root, and registered here
// under scene.Name. The m.Transform an Entity is
// drawn at is not among them: it is the ecs plugin's, and every binding reads
// the same Store.
func (plugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Model](registrar, drawableReserve)
	ecs.RegisterComponent[Mesh](registrar, drawableReserve)
	ecs.RegisterComponent[Animation](registrar, drawableReserve)
	ecs.RegisterComponent[Params](registrar, drawableReserve)
	ecs.RegisterComponent[Material](registrar, drawableReserve)
	ecs.RegisterComponent[Light](registrar, lightReserve)
	ecs.RegisterComponent[Camera](registrar, cameraReserve)
	registrar.InitResource(newScratch())
	registrar.InitResource(newKeyScratch())
	registrar.Subscribe[LoadOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, loadSystem)).
		Before[RecordOnUpdate]()
	registrar.Subscribe[RecordOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, recordSystem))
	// storage installs the mount at its Start, ahead of every plugin that
	// depends on it, so the debug shader is in place for the first frame.
	registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
		Id: shaderMountID, Priority: math.MaxInt, FS: shaderFS,
	})
	registerDebug(registrar)
	return nil
}

// registerDebug declares the five debug shapes, their scratches and their ten
// Systems, chained one after another from scene.DebugOnUpdate and every
// one before scene.LoadOnUpdate. The chain costs no parallelism: every one
// of them writes model's Lookup, as the load System does, so they could not
// overlap each other or it anyway.
func registerDebug(registrar *kernel.Registrar) {
	ecs.RegisterComponent[DebugBox](registrar, debugReserve)
	ecs.RegisterComponent[DebugSphere](registrar, debugReserve)
	ecs.RegisterComponent[DebugPlane](registrar, debugReserve)
	ecs.RegisterComponent[DebugLine](registrar, debugReserve)
	ecs.RegisterComponent[DebugWireBox](registrar, debugReserve)
	registrar.InitResource(newDebugScratch[DebugBox]())
	registrar.InitResource(newDebugScratch[DebugSphere]())
	registrar.InitResource(newDebugScratch[DebugPlane]())
	registrar.InitResource(newDebugScratch[DebugLine]())
	registrar.InitResource(newDebugScratch[DebugWireBox]())

	handler := func(system any) func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		return ecs.ToHandler[app.UpdateEvent](registrar, system)
	}
	registrar.Subscribe[DebugOnUpdate](handler(debugBoxKind.changeSystem)).
		Before[LoadOnUpdate]()
	registrar.Subscribe[debugBoxBakeOnUpdate](handler(debugBoxKind.bakeSystem)).
		After[DebugOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugSphereChangeOnUpdate](handler(debugSphereKind.changeSystem)).
		After[debugBoxBakeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugSphereBakeOnUpdate](handler(debugSphereKind.bakeSystem)).
		After[debugSphereChangeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugPlaneChangeOnUpdate](handler(debugPlaneKind.changeSystem)).
		After[debugSphereBakeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugPlaneBakeOnUpdate](handler(debugPlaneKind.bakeSystem)).
		After[debugPlaneChangeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugLineChangeOnUpdate](handler(debugLineKind.changeSystem)).
		After[debugPlaneBakeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugLineBakeOnUpdate](handler(debugLineKind.bakeSystem)).
		After[debugLineChangeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugWireBoxChangeOnUpdate](handler(debugWireBoxKind.changeSystem)).
		After[debugLineBakeOnUpdate]().Before[LoadOnUpdate]()
	registrar.Subscribe[debugWireBoxBakeOnUpdate](handler(debugWireBoxKind.bakeSystem)).
		After[debugWireBoxChangeOnUpdate]().Before[LoadOnUpdate]()
}
