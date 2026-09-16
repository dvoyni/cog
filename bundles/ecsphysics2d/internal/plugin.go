package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
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

	// polygon is the run Index copies a Polygon Component's vertices into
	// before handing them to an Insert, refilled once per ShapePoly Entity and
	// never read outside that System. It lives here because a local would be
	// nil at the top of every tick and would allocate on the hot path; ecs.List
	// hands out no slice, so there is nothing to point at instead.
	polygon []m.Vec2d
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
	// Joints are their own Entities and there are far fewer of them than there
	// are Bodies: a jointed figure of limbs is a dozen, and the cost estimates
	// the design is built on are quoted at 500.
	jointReserve = 512
)

// Register resolves the settings, declares the seven Components a Body is made
// of and the Joint that holds two of them, publishes the two indices and the
// two solver Resources, registers ShrinkCmd, and chains the four Systems in
// cp's order.
//
// The chain is explicit rather than left to the locks. Integrate and Solve
// would serialise on Velocity anyway, Index reads the Position Integrate
// writes, and Detect writes the Contact list Solve reads — but the order is
// contract rather than a consequence: an app writes Before[IntegrateOnUpdate]
// and After[DetectOnUpdate]().Before[SolveOnUpdate]() against it, and those
// orderings must keep meaning what they mean whatever the lock sets become.
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
	// Shape takes the larger of the two reserves, because it is the one
	// Component both kinds of Body carry: its population is the statics plus
	// the shaped movers, and static geometry is the bigger half of that in
	// every scene anyone has measured. A reserve is a hint, not a cap.
	ecs.RegisterComponent[ecsphysics2d.Shape](registrar, staticReserve)
	// Polygon takes the smaller reserve: it is the second Component only a
	// Shape of more than four vertices needs, and a scene whose every Shape is
	// one is not a scene anyone has measured.
	ecs.RegisterComponent[ecsphysics2d.Polygon](registrar, bodyReserve)
	// The Joint is the one Component that is not a Body's: it is carried by an
	// Entity of its own, holding two Bodies by Reference, because a Body may be
	// held by several Joints and a Component is one per Entity.
	ecs.RegisterComponent[ecsphysics2d.Joint](registrar, jointReserve)

	// The two indices, at the cell sizes the settings resolved — two named types
	// so that their locks stay apart: rebuilding the Bodies write-locks only the
	// Bodies, and a line-of-sight Probe on the statics never waits for it.
	registrar.InitResource(ecsphysics2d.NewStaticIndex(resolved.staticCellSize))
	registrar.InitResource(ecsphysics2d.NewBodyIndex(resolved.bodyCellSize))

	// The Contact list, seeded with the one source of randomness in the
	// package: the direction two exactly coincident Shapes are parted along.
	registrar.InitResource(types.NewContacts(resolved.seed))

	// The pairs a Joint holds apart, rebuilt by Index and read by Detect. It is
	// a Resource of its own so that the Joint walk's write does not have to be
	// held through detection.
	registrar.InitResource(types.NewJointedPairs())

	// The one Command physics has: giving the buffers back after a spike. It is
	// registered here rather than reached through a Resource because releasing
	// memory must exclude the Systems that hold those buffers, and a Command's
	// lock is the only thing that does.
	registrar.HandleCommand[ecsphysics2d.ShrinkCmd](types.ShrinkCommand)

	registrar.Subscribe[ecsphysics2d.IntegrateOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, integrate, step()))
	registrar.Subscribe[ecsphysics2d.IndexOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.index)).
		After[ecsphysics2d.IntegrateOnUpdate]()
	registrar.Subscribe[ecsphysics2d.DetectOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.detect, step())).
		After[ecsphysics2d.IndexOnUpdate]()
	registrar.Subscribe[ecsphysics2d.SolveOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.solve, step())).
		After[ecsphysics2d.DetectOnUpdate]()
	return nil
}

// step is the projection of one tick's fixed timestep, in seconds, out of the
// event. A Feeder handed to two Systems is refused, so each registration site
// builds its own.
func step() ecs.Feeder[app.UpdateEvent] {
	return ecs.Feed(func(event app.UpdateEvent) float64 { return event.Dt })
}
