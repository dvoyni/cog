package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

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

// plugin registers the Components a Body is made of and chains the five Systems
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
	// nil at the top of every tick and would allocate on the hot path; m.List
	// hands out no slice, so there is nothing to point at instead.
	polygon []m.Vec2d

	// woken is the run of Bodies whose Sleeping Tag went since the last Index,
	// which Index takes out of the sleepers' grid in one pass. It lives here
	// for the reason polygon does.
	woken []ecs.Entity
}

// New makes the physics plugin. Register ecs beside it, and app above it: the
// five Systems are subscriptions on app.UpdateEvent, fed the fixed step.
//
//	kernel.New(config).WithPlugins(
//	    appplugin.New(), platformplugin.New(),
//	    ecsplugin.New(), ecsphysics2dplugin.New(), game.New())
//
// Its settings arrive, as every plugin's do, through kernel.New's config map
// under ecsphysics2d.Name, so New takes no arguments.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins physics requires: ecs, because registering a
// Component and building a System both reach the id authority, and app, because
// the step is app.UpdateEvent's fixed timestep.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, app.Name}
}

// Register resolves the settings, declares the Components a Body is made of —
// the Sleeping Tag and the Rest the sleep System keeps among them — and the
// Joint that holds two of them, publishes the two indices, the two solver
// Resources, the Constants and the Sleep settings, registers ShrinkCmd and
// WakeCmd, and chains the five Systems in cp's order.
//
// The chain is explicit rather than left to the locks. Integrate and Solve
// would serialise on Velocity anyway, Index reads the Position Integrate
// writes, and Detect writes the Contact list the sleep System and Solve write
// after it — but the order is contract rather than a consequence: an app writes
// Before[IntegrateOnUpdate] and After[DetectOnUpdate]().Before[SolveOnUpdate]()
// against it, and those orderings must keep meaning what they mean whatever the
// lock sets become.
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

	ecs.RegisterComponent[Position](registrar, bodyReserve)
	ecs.RegisterComponent[Velocity](registrar, bodyReserve)
	ecs.RegisterComponent[Force](registrar, bodyReserve)
	ecs.RegisterComponent[Dynamic](registrar, bodyReserve)
	ecs.RegisterComponent[Static](registrar, staticReserve)
	// Sleeping is a Tag only the sleep System adds and removes, and Rest is
	// what that System keeps for each Dynamic body, which no app can name.
	// Both are the moving Bodies' population, and neither is a Component until
	// sleeping is turned on.
	ecs.RegisterComponent[Sleeping](registrar, bodyReserve)
	ecs.RegisterComponent[Rest](registrar, bodyReserve)
	// Shape takes the larger of the two reserves, because it is the one
	// Component both kinds of Body carry: its population is the statics plus
	// the shaped movers, and static geometry is the bigger half of that in
	// every scene anyone has measured. A reserve is a hint, not a cap.
	ecs.RegisterComponent[Shape](registrar, staticReserve)
	// Polygon takes the smaller reserve: it is the second Component only a
	// Shape of more than four vertices needs, and a scene whose every Shape is
	// one is not a scene anyone has measured.
	ecs.RegisterComponent[Polygon](registrar, bodyReserve)
	// The Joint is the one Component that is not a Body's: it is carried by an
	// Entity of its own, holding two Bodies by Reference, because a Body may be
	// held by several Joints and a Component is one per Entity.
	ecs.RegisterComponent[Joint](registrar, jointReserve)

	// The two indices, at the cell sizes the settings resolved — two named types
	// so that their locks stay apart: rebuilding the Bodies write-locks only the
	// Bodies, and a line-of-sight Probe on the statics never waits for it.
	registrar.InitResource(NewStaticIndex(resolved.staticCellSize))
	registrar.InitResource(NewBodyIndex(resolved.bodyCellSize))

	// The Contact list, seeded with the one source of randomness in the
	// package: the direction two exactly coincident Shapes are parted along.
	registrar.InitResource(NewContacts(resolved.seed))

	// The Constants, at the plugin's defaults: no gravity, which is a top-down
	// plane. An app that wants others writes them from a System of its own,
	// through ecs.Write[*Constants], and Solve reads them every tick.
	registrar.InitResource(&Constants{})

	// Sleeping, off: an app turns it on by writing Sleep, and until then the
	// sleep System builds nothing.
	registrar.InitResource(&Sleep{})

	// The WakeCmd's queue, a Resource of its own so that the command's lock is
	// a write on it and on nothing the step reads.
	registrar.InitResource(NewWakes())

	// The pairs a Joint holds apart, rebuilt by Index and read by Detect. It is
	// a Resource of its own so that the Joint walk's write does not have to be
	// held through detection.
	registrar.InitResource(NewJointedPairs())

	// The one Command physics has: giving the buffers back after a spike. It is
	// registered here rather than reached through a Resource because releasing
	// memory must exclude the Systems that hold those buffers, and a Command's
	// lock is the only thing that does.
	registrar.HandleCommand[ShrinkCmd](ShrinkCommand)

	// Waking an Island from outside a touch or a write, queued for the sleep
	// System, which is the one that wakes it.
	registrar.HandleCommand[WakeCmd](WakeCommand)

	registrar.Subscribe[IntegrateOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, integrateSystem, step()))
	registrar.Subscribe[IndexOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.indexSystem)).
		After[IntegrateOnUpdate]()
	registrar.Subscribe[DetectOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.detectSystem, step())).
		After[IndexOnUpdate]()
	registrar.Subscribe[SleepOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.sleepSystem, step())).
		After[DetectOnUpdate]()
	registrar.Subscribe[SolveOnUpdate](
		ecs.ToHandler[app.UpdateEvent](registrar, p.solveSystem, step())).
		After[SleepOnUpdate]()
	return nil
}

// indexSystem rebuilds the two spatial indices from the positions Integrate has
// just written, drains the Shape hooks that say which Static Entities came and
// went, and rebuilds the set of pairs a Joint holds apart.
//
// The two halves are not symmetric, and that is the whole design. Statics are
// maintained incrementally, world-cached once at Insert and never again, which
// is why moving a Static means replacing the Entity; the Bodies are Cleared and
// refilled whole every tick from wherever Integrate has just put them. Clear
// keeps every buffer it has grown, so the rebuild allocates nothing.
//
// It stays a System of its own rather than folding into Detect, because its
// index writes are held only for the rebuild — about 9 µs for 1 024 Bodies —
// while detection, the heavy part, runs under reads that a gameplay query can
// overlap. One System doing both would hold the index write through detection.
//
// A ShapePoly's vertices come out of the Polygon Component beside the Shape,
// probed with ecs.Get and copied into the plugin's own scratch run once per
// Entity: m.List yields copies and hands out no slice, deliberately, so there
// is nothing to point at. Every other kind carries its geometry in the Shape
// value itself and reads nil. The scratch lives on the plugin rather than in
// this frame so that the run it has grown survives the tick — a local would be
// nil again at every call and would allocate every tick.
func (p *plugin) indexSystem(
	shapes *ecs.Hooks[Shape, ecs.HookAddedRemoved],
	sleepers *ecs.Hooks[Sleeping, ecs.HookAddedRemoved],
	shaped *ecs.Get[Shape],
	asleep *ecs.Get[Sleeping],
	places *ecs.Get[Position],
	statics *ecs.Get[Static],
	polygons *ecs.Get[Polygon],
	bodies *ecs.Query[bodyIndexQuery],
	every *ecs.Query[everyBodyIndexQuery],
	tagged *ecs.Query[SleeperQuery],
	joints *ecs.Query[jointIndexQuery],
	staticIndex *ecs.Write[*StaticIndex],
	bodyIndex *ecs.Write[*BodyIndex],
	jointed *ecs.Write[*JointedPairs],
) {
	static := staticIndex.Get()
	for entity, hook := range shapes.All() {
		// The removal is unconditional, and it has to be. A Despawn empties
		// every Store before the record is read, so the Static Tag is already
		// gone by the time this asks, and a removal gated on the Tag would be
		// dropped and leave the Entity in the index for ever. Remove does
		// nothing when the index does not hold the Entity, which is what makes
		// the unconditional call free for every Body's Shape.
		if hook.IsRemoved() {
			static.Remove(entity)
			// A Sleeping body that lost its Shape leaves the sleepers' grid
			// too. One that was despawned is taken out by its Sleeping hook
			// below, which is why this asks whether the Tag is still there.
			if _, sleeping := asleep.Of(entity); sleeping {
				p.woken = append(p.woken, entity)
			}
		}
		if hook.IsAdded() {
			// A Hook narrows by nothing but its Component and its kind set, so
			// the Static half of "which index is this Shape's" is checked here,
			// with Get keyed by Entity — the pattern hooks.md's "No filters"
			// prescribes. Without it a Body's Shape would be inserted into the
			// static index at its spawn position and stay there, frozen, while
			// the Body itself moved on in BodyIndex.
			if _, ok := statics.Of(entity); !ok {
				continue
			}
			// A Static whose Shape arrived before its Position is skipped, and
			// skipped silently: there is no second chance, because a Static
			// enters the index on its hook and nowhere else. This is the
			// package's stated-not-checked stance — it has no validity checks
			// and does not borrow the ECS's Validation mode for them — and the
			// way to avoid it is to spawn a Static's Shape, Position and Tag
			// together, which is what an ordinary Spawn does.
			if place, ok := places.Of(entity); ok {
				static.Insert(entity, hook.Value, place.Current, place.Angle,
					p.polygonVerts(polygons, entity, hook.Value))
			}
		}
	}

	body := bodyIndex.Get()

	// The sleepers' grid is kept current rather than rebuilt: a Body whose
	// Island fell asleep since the last Index goes in, where it sleeps, and one
	// whose Island woke, or that was despawned asleep, comes out. The removals
	// are gathered first and made in one pass over the grid, which keeps no
	// Entity to slot table; an addition is checked against the Tag and the
	// Components still being there, so a Body that fell asleep and was
	// despawned before this ran is never put in.
	for entity, hook := range sleepers.All() {
		if hook.IsRemoved() {
			p.woken = append(p.woken, entity)
		}
	}
	RemoveSleepers(body, p.woken)
	p.woken = p.woken[:0]
	for entity, hook := range sleepers.All() {
		if !hook.IsAdded() {
			continue
		}
		if _, sleeping := asleep.Of(entity); !sleeping {
			continue
		}
		shape, okShape := shaped.Of(entity)
		place, okPlace := places.Of(entity)
		if okShape && okPlace {
			InsertSleeper(body, entity, shape, place.Current, place.Angle,
				p.polygonVerts(polygons, entity, shape))
		}
	}

	// InsertMoving rather than Insert, so that a moving Sensor carries
	// the path Detect Probes it along. The previous pose comes off the Position
	// this walk already reads, which is what keeps the swept Sensor from
	// costing any System a lock it did not already hold: Detect names no
	// Component Store at all and still does not.
	//
	// The walk is named twice, with the Sleeping filter and without, for the
	// reason integrateSystem's is.
	body.Clear()
	if NobodySleeps(tagged) {
		for entity, it := range every.All() {
			body.InsertMoving(
				entity, it.Shape,
				it.Place.Current, it.Place.Previous, it.Place.Angle, it.Place.PreviousAngle,
				p.polygonVerts(polygons, entity, it.Shape),
			)
		}
	} else {
		for entity, it := range bodies.All() {
			body.InsertMoving(
				entity, it.Shape,
				it.Place.Current, it.Place.Previous, it.Place.Angle, it.Place.PreviousAngle,
				p.polygonVerts(polygons, entity, it.Shape),
			)
		}
	}

	// A Joint whose two Bodies still collide contributes nothing, so the set
	// stays empty for every scene that has no such Joint and Detect's check
	// stays one branch. A dangling Reference is added like any other pair: the
	// pair simply never comes up.
	pairs := jointed.Get()
	pairs.Clear()
	for _, it := range joints.All() {
		if !it.Joint.CollideBodies {
			pairs.Add(it.Joint.A, it.Joint.B)
		}
	}
}

// polygonVerts is the run of local vertices an Insert takes: nil for every kind
// but ShapePoly, and the Polygon Component copied into the plugin's scratch for
// that one. A ShapePoly with no Polygon beside it copies nothing and is listed
// in no cell, which is this package's stated-not-checked stance — the way to
// avoid it is to spawn the Shape and the Polygon the constructor built together.
func (p *plugin) polygonVerts(
	polygons *ecs.Get[Polygon], entity ecs.Entity, shape Shape,
) []m.Vec2d {
	if shape.Kind != ShapePoly {
		return nil
	}
	polygon, ok := polygons.Of(entity)
	if !ok {
		return nil
	}
	p.polygon = PolygonVerts(p.polygon[:0], shape, polygon)
	return p.polygon
}

// detectSystem finds the tick's Contacts by walking the two indices, and is
// where the seeded coincidence nudge lives.
//
// It names no Component Store. cp's narrowphase reaches the Shape and the Body
// through pointers; the port's index entries carry the Shape, its world cache
// and the transform the position built, so detection reads them and nothing
// else. The lock set is the two indices for read and the Contact list for
// write, which is strictly less than the specification's table allows itself.
//
// An app's filter Systems — cp's Begin and PreSolve — order themselves
// After[DetectOnUpdate]().Before[SolveOnUpdate]() and take
// *ecs.Write[*Contacts].
func (p *plugin) detectSystem(
	staticIndex *ecs.Read[*StaticIndex],
	bodyIndex *ecs.Read[*BodyIndex],
	jointed *ecs.Read[*JointedPairs],
	contacts *ecs.Write[*Contacts],
	step *ecs.In[float64],
) {
	Collide(
		contacts.Get(), bodyIndex.Get(), staticIndex.Get(), jointed.Get(),
		p.settings.persistenceTicks(step.Get()), p.settings.slop,
	)
}

// solveSystem is the indivisible half of the step: the dense solved-Contact
// list, the gather through the BodyIndex slot table, PreStep, the velocity
// integration, the warm start, the iterations, and the bias applied as a
// position delta.
//
// Velocity integration cannot be a System of its own, which is what makes Solve
// indivisible, and both sides force it: PreStep computes bounce from the
// velocity before integration, which is what stops gravity-fed jitter from
// eating Restitution, and the warm start must follow damping, or the cached
// Impulse is damped away before it does anything.
//
// Position is written here as well as by Integrate, which costs no parallelism:
// the two are links of the same chain, so nothing that could have run beside
// one could have run beside the other.
//
// Gravity is read out of Constants once a tick, here, and handed to every
// Body's velocity integration, rather than read per Body. The read is shared,
// so it costs no parallelism either: nothing the plugin registers writes
// Constants, and the only System that waits on this read is an app's own that
// took ecs.Write[*Constants], which is the price that app chose.
func (p *plugin) solveSystem(
	bodies *ecs.Query[VelocityQuery],
	every *ecs.Query[EveryVelocityQuery],
	sleepers *ecs.Query[SleeperQuery],
	joints *ecs.Query[JointQuery],
	dynamics *ecs.Get[Dynamic],
	velocities *ecs.Set[Velocity],
	places *ecs.Set[Position],
	contacts *ecs.Write[*Contacts],
	constants *ecs.Read[*Constants],
	sleeping *ecs.Get[Sleeping],
	step *ecs.In[float64],
) {
	Solve(
		contacts.Get(), bodies, every, sleepers, joints, dynamics, velocities, places, sleeping,
		constants.Get().Gravity,
		step.Get(), p.settings.iterations, p.settings.slop, p.settings.bias,
	)
}

// sleepSystem is cp's ProcessComponents, between Detect and Solve: every awake
// Dynamic body's idle time kept, every sleeping Island something disturbed
// woken, and the tick's Islands built from the Contacts and the Joints so that
// the ones idle for Sleep.Time fall asleep.
//
// Its lock set is its own and nothing else's changes for it: read on Sleep,
// Constants, Dynamic, Velocity, Position, Joint and both indices; write on
// Force, which it clears on a sleeper as Solve clears it on an awake Body, on
// the Sleeping and Rest Stores, which only it writes, on the Contact list,
// whose quiet Contacts it moves, and on the WakeCmd's queue. Every System that
// filters on Sleeping gains a read of a Store only this one writes, and this
// one sits in the chain between Detect and Solve, both of which already
// serialise with everything it writes. Constants is read as Solve reads it:
// the only System that waits on it is an app's own that took the write.
//
// It never writes the Body index. Moving a Body into the sleepers' grid and out
// again is Index's, on the next tick, off the Sleeping Tag's own hook; a query
// asks both grids, so nothing it answers changes in between.
func (p *plugin) sleepSystem(
	settings *ecs.Read[*Sleep],
	constants *ecs.Read[*Constants],
	awake *ecs.Query[AwakeQuery],
	asleep *ecs.Query[AsleepQuery],
	joints *ecs.Query[IslandJointQuery],
	rests *ecs.Set[Rest],
	forces *ecs.Set[Force],
	velocities *ecs.Get[Velocity],
	places *ecs.Get[Position],
	tag *ecs.Set[Sleeping],
	untag *ecs.Remove[Sleeping],
	contacts *ecs.Write[*Contacts],
	wakes *ecs.Write[*Wakes],
	step *ecs.In[float64],
) {
	sleep := settings.Get()
	ProcessIslands(
		contacts.Get(), awake, asleep, joints, rests, forces, velocities, places, tag, untag,
		wakes.Get(),
		sleep.IdleSpeed, sleep.Time, constants.Get().Gravity, step.Get(),
	)
}

// step is the projection of one tick's fixed timestep, in seconds, out of the
// event. A Feeder handed to two Systems is refused, so each registration site
// builds its own.
func step() ecs.Feeder[app.UpdateEvent] {
	return ecs.Feed(func(event app.UpdateEvent) float64 { return event.Dt })
}
