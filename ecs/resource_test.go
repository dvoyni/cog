package ecs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// drawLog stands in for scene's *scene.OpQueue: the frame-local resource a
// bound plugin publishes and a recording System appends to. It is re-recorded
// from nothing every frame and holds no per-entity state, which is why the
// Components are the source of truth and there is nothing to mirror.
type drawLog struct{ Xs []float32 }

// modelNames stands in for scene's *scene.Names: a table a recording System
// reads and never writes.
type modelNames struct{ Scale float32 }

// bindingPlugin is the third plugin a binding necessarily is. ecs imports only
// kernel and the bound plugin imports nothing of ecs, so neither can know about
// the other; the binding imports both and registers ordinary Systems.
type bindingPlugin struct {
	log   *drawLog
	names *modelNames
}

func (p *bindingPlugin) Name() kernel.PluginName { return "binding" }

func (p *bindingPlugin) Dependencies() []kernel.PluginName { return nil }

func (p *bindingPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(p.log)
	registrar.InitResource(p.names)
	return nil
}

// boundDeps is the systems plugin's dependency list when its Systems name the
// bound plugin's resources: locking a cell someone else owns is exactly what
// the coupling check is about.
var boundDeps = []kernel.PluginName{Name, "components", "binding"}

// TestASystemReachesAnotherPluginsResourceThroughItsSignature is the whole of
// the binding mechanism: no binding type, no adapter, no registration call of
// the ECS's own — a resource another plugin published arrives as a parameter,
// and the lock it needs was declared at registration beside the Query's Stores.
func TestASystemReachesAnotherPluginsResourceThroughItsSignature(t *testing.T) {
	type recordSystem kernel.Subscription[app.UpdateEvent]

	log, names := &drawLog{}, &modelNames{Scale: 10}
	entities, components, engine := newWorldWith(t, 64,
		func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[recordSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[moveQuery], table *Read[*modelNames], out *Write[*drawLog]) {
					scale, queue := table.Get().Scale, out.Get()
					queue.Xs = queue.Xs[:0]
					for _, it := range q.All() {
						queue.Xs = append(queue.Xs, it.Body.X*scale)
					}
				}))
		},
		boundDeps, &bindingPlugin{log: log, names: names})

	e := entities.alloc()
	components.bodies.Set(e, body{X: 3})
	components.velocities.Set(e, velocity{X: 1})

	frame(t, engine, 1)

	if len(log.Xs) != 1 || log.Xs[0] != 30 {
		t.Fatalf("the recording System wrote %v, want [30]", log.Xs)
	}

	// The resource joins the lock set beside the Query's Stores, at
	// registration, as visible in the signature as a Component is.
	description := engine.Describe()
	for _, sub := range description.Subscriptions {
		if sub.Type != reflect.TypeFor[recordSystem]() {
			continue
		}
		if !namesType(sub.Reads, "*ecs.modelNames") {
			t.Fatalf("the recording System reads %v, which does not include *ecs.modelNames", sub.Reads)
		}
		if !namesType(sub.Writes, "*ecs.drawLog") {
			t.Fatalf("the recording System writes %v, which does not include *ecs.drawLog", sub.Writes)
		}
		if namesType(sub.Reads, "*ecs.drawLog") {
			t.Fatalf("the recording System read-locks *ecs.drawLog beside writing it: %v", sub.Reads)
		}
		return
	}
	t.Fatalf("no recordSystem subscription in the description")
}

// TestAResourceHandleIsRefreshedPerTick holds the kernel's standing rule at the
// ECS's own boundary: the handle is a window onto the cell the lock covers, so
// a value the bound plugin replaces wholesale is seen on the next tick rather
// than a stale one captured at registration.
func TestAResourceHandleIsRefreshedPerTick(t *testing.T) {
	type resetSystem kernel.Subscription[app.UpdateEvent]

	first, second := &modelNames{Scale: 1}, &modelNames{Scale: 2}
	var seen []float32
	_, _, engine := newWorldWith(t, 8,
		func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[resetSystem](ToHandler[app.UpdateEvent](world,
				func(table *Write[*modelNames]) {
					seen = append(seen, table.Get().Scale)
				}))
			// The one System that reassigns the cell, and the one place Set
			// belongs: most resources are pointers mutated in place.
			registrar.Subscribe[replaceSystem](ToHandler[replaceEvent](world,
				func(replace replaceEvent, table *Write[*modelNames]) {
					table.Set(replace.with)
				}))
		},
		boundDeps, &bindingPlugin{log: &drawLog{}, names: first})

	frame(t, engine, 1)
	// Replacing the cell's value is what Write.Set is for, and it is the one
	// case a handle captured at registration would get wrong.
	if err := engine.Executioner().PublishEvent(replaceEvent{with: second}).Wait(); err != nil {
		t.Fatalf("publishing the replacement: %v", err)
	}
	frame(t, engine, 1)

	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("the System saw %v across two ticks, want [1 2]", seen)
	}
}

// replaceEvent drives the one subscription that reassigns the resource cell.
type replaceEvent struct{ with *modelNames }

type replaceSystem kernel.Subscription[replaceEvent]

// TestAResourceHandleRefusesTheECSsOwnCells keeps the one route to a Store a
// handle that declares read{*Entities} and checks the Component is registered.
// Store.Remove is exported, so a read handle on a Store would be a write in
// disguise; the accessors exist precisely so it never has to be.
func TestAResourceHandleRefusesTheECSsOwnCells(t *testing.T) {
	for _, probe := range []struct {
		name   string
		system any
		wanted string
	}{
		{"read a Store", func(s *Read[*Store[body]]) {}, "ecs.Get["},
		{"write a Store", func(s *Write[*Store[body]]) {}, "ecs.Set["},
		{"write the authority", func(w *Write[*Entities]) {}, "ecs.WriteableEntities"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			message := composeAndFail(t, func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[guardSystem](ToHandler[app.UpdateEvent](world, probe.system))
			})
			if !strings.Contains(message, probe.wanted) {
				t.Fatalf("composition failure %q does not point at %s", message, probe.wanted)
			}
		})
	}
}

// composeAndFail composes an engine whose Systems are expected to be refused at
// registration and returns the composition failure's sentence. A bad signature
// is a panic in Register, which the plugin boundary reports as ErrPluginPanic
// naming the plugin — the third of the three answers the specs record, taken
// here because the alternative needs kernel to grow a way for a plugin-side
// builder to reach its private error list.
func composeAndFail(t *testing.T, subscribe func(*kernel.Registrar, *Entities)) string {
	t.Helper()
	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&bindingPlugin{log: &drawLog{}, names: &modelNames{}},
			&systemsPlugin{world: entities, deps: boundDeps, subscribe: subscribe},
		)
	if failure == nil {
		t.Fatalf("composing the System succeeded")
	}
	return failure.Error()
}
