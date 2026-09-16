package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// plugin registers the Components a Body is made of and chains the four Systems
// the step is. It holds the resolved settings and nothing else: a Component is
// the source of truth and nothing is mirrored, so there is no world object for
// a plugin to be.
type plugin struct {
	// settings is the Config with its defaults filled in, fixed at Register and
	// never written again. The Systems close over it.
	settings settings
}

// New makes the physics plugin. Register ecs beside it, and app above it: the
// four Systems are subscriptions on app.UpdateEvent, fed the fixed step.
//
//	kernel.New(config).WithPlugins(
//	    appplugin.New(), platformplugin.New(),
//	    ecsplugin.New(), ecsphysics2dplugin.New(), game.New())
//
// Its settings arrive, as every plugin's do, through kernel.New's config map
// under ecsphysics2d.Name, so New takes no arguments.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return ecsphysics2d.Name }

// Dependencies reports the plugins physics requires: ecs, because registering a
// Component and building a System both reach the id authority, and app, because
// the step is app.UpdateEvent's fixed timestep.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, app.Name}
}

// The populations the Stores reserve for. They are hints, not caps: a Store
// grows by doubling past its reserve. Static Entities outnumber moving ones in
// every scene anyone has measured, and a Body that never moves carries neither
// Velocity nor Force.
const (
	bodyReserve   = 1024
	staticReserve = 4096
)

// Register resolves the settings, declares the five Components a Body is made
// of, and chains the four Systems in cp's order.
//
// The chain is explicit rather than left to the locks. Integrate and Solve
// would serialise on Velocity anyway, but Index and Detect are empty today and
// so lock nothing that would order them, and the order is contract: an app
// writes Before[IntegrateOnUpdate] and After[DetectOnUpdate] against it now, and
// those orderings must keep meaning what they mean once the two are filled in.
//
// Every System is fed the step the same way, with ecs.Feed projecting
// UpdateEvent.Dt, so none of them names where its step came from. Physics runs
// on every published tick, catch-up ticks included: a skipped step is a
// different simulation, not a cheaper one.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	resolved, err := resolveConfig(config)
	if err != nil {
		return err
	}
	p.settings = resolved

	ecs.RegisterComponent[ecsphysics2d.Position](registrar, bodyReserve)
	ecs.RegisterComponent[ecsphysics2d.Velocity](registrar, bodyReserve)
	ecs.RegisterComponent[ecsphysics2d.Force](registrar, bodyReserve)
	ecs.RegisterComponent[ecsphysics2d.Dynamic](registrar, bodyReserve)
	ecs.RegisterComponent[ecsphysics2d.Static](registrar, staticReserve)

	registrar.Subscribe[ecsphysics2d.IntegrateOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, integrate, step()))
	registrar.Subscribe[ecsphysics2d.IndexOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, index)).
		After[ecsphysics2d.IntegrateOnUpdate]()
	registrar.Subscribe[ecsphysics2d.DetectOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, detect)).
		After[ecsphysics2d.IndexOnUpdate]()
	registrar.Subscribe[ecsphysics2d.SolveOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, solve, step())).
		After[ecsphysics2d.DetectOnUpdate]()
	return nil
}

// step is the projection of one tick's fixed timestep, in seconds, out of the
// event. A Feeder handed to two Systems is refused, so each registration site
// builds its own.
func step() ecs.Feeder[app.UpdateEvent] {
	return ecs.Feed(func(event app.UpdateEvent) float64 { return event.Dt })
}
