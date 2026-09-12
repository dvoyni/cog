package ecsscene

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/scene"
)

// Name is the binding plugin's kernel name and configuration key.
const Name kernel.PluginName = "ecsscene"

// RecordEventHandler is the recording System's subscription. It is exported so
// that a game System which moves drawables can order itself Before it, the way
// scene exports its own flush.
//
// It declares no ordering of its own, and that is the demonstration rather than
// an omission: scene's flush is subscribed Last().Before[gfx.UpdateEventHandler],
// so anything that does not ask to be last already runs before it. The binding
// needed no new ordering vocabulary.
type RecordEventHandler kernel.Subscription[app.UpdateEvent]

// Plugin is cog's ecs↔scene binding: the third plugin two plugins that cannot
// import each other are bound by.
//
// Register ecs and scene before it. It owns three Component types and one
// read-only Manifest resource, and it subscribes exactly one System.
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

// Dependencies reports both halves of the binding. It locks scene's queue and
// the ECS's authority, so it declares both; the Go import graph already forces
// the same two edges.
func (p *Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name}
}

// Register declares the Components, publishes the Manifest and subscribes the
// one recording System. A Component is registered by the plugin that defines
// its Go type, which is what keeps cog's coupling check working on Component
// data: a System elsewhere that locks one of these Stores must declare a
// dependency on this plugin.
func (p *Plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	names := &manifest{}
	if err := names.fill(config); err != nil {
		return err
	}
	registrar.InitResource(names)
	ids := uint32(config.Drawables)
	ecs.RegisterComponent[Transform](registrar, p.world, ids)
	ecs.RegisterComponent[Drawable](registrar, p.world, ids)
	ecs.RegisterComponent[Animation](registrar, p.world, ids)
	registrar.Subscribe[RecordEventHandler](recordDraws(p.world))
	return nil
}

// drawQuery is what the recording System iterates: every Entity having both a
// place to stand and something to draw. Both fields are read — a value field
// yields a copy — because recording changes nothing about an Entity.
//
// Animation is deliberately not a field. A Query matches an Entity having at
// least the Components it names, so naming Animation here would drop every
// unanimated drawable out of the walk.
type drawQuery struct {
	Place Transform
	Draw  Drawable
}

// recordDraws builds the one recording System, closing over the scratch its
// variable-length draw data is rebuilt in.
//
// One recording System per bound plugin is the shape. A second one would
// serialise against this one whatever Components it read — *scene.OpQueue is
// one resource, so scene recording is one lock wide — and it would cost a
// scheduling slot to do it.
//
// The scratch is allocated once, at registration, and captured by the System's
// closure. That is safe for exactly one System: two Systems sharing one scratch
// have no lock between them. This one is safe because it is the only holder and
// because the System takes scene's queue for write, so two publications of the
// same tick cannot run it concurrently either — the lock that orders the queue
// orders the scratch with it. A scratch shared any more widely belongs in a
// resource, which is what puts it in the lock set.
func recordDraws(world *ecs.Entities) func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	plays := make([]scene.ClipPlay, 0, MaxPlays)
	return ecs.ToHandler[app.UpdateEvent](world, func(
		q *ecs.Query[drawQuery],
		animations *ecs.Get[Animation],
		names *ecs.Read[*Manifest],
		out *ecs.Write[*scene.OpQueue],
	) {
		// Both handles are read once, outside the loop. Get goes to the cell the
		// lock covers on every call, and neither value is a place to keep
		// anything: they are valid for the body of this System and no longer.
		table, queue := names.Get(), out.Get()
		for e, it := range q.All() {
			path, known := table.Model(it.Draw.Model)
			if !known {
				continue
			}
			// The transform is rebuilt by value, field by field. Handing scene a
			// *m.Mat4 into the Store would be read at the flush, which is a
			// different System running after this one's locks are gone.
			draw := scene.ModelDraw{Transform: scene.Transform{
				Position: it.Place.Position,
				Rotation: it.Place.Rotation,
				Scale:    it.Place.Scale,
			}}
			// An Accessor is how an optional Component is reached: one probe, on
			// a Store this System's signature named, under a lock it already
			// holds.
			if animation, animated := animations.Of(e); animated {
				plays = table.clipPlays(plays, &animation)
				draw.Plays = plays
			}
			queue.Model(it.Draw.Layers, path, draw)
		}
	})
}
