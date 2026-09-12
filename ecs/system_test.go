package ecs

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

type body struct{ X, Y float32 }

type velocity struct{ X, Y float32 }

type collider struct{ Radius float32 }

// moveQuery is the two-Component Query of the tracer bullet: a pointer field
// writes and yields the stored value, a value field reads and yields a copy.
type moveQuery struct {
	Body     *body
	Velocity velocity
}

type moveSystem kernel.Subscription[app.UpdateEvent]

// componentsPlugin owns the Component types, which is the rule: a Component is
// registered by the plugin that defines its Go type, and that is what keeps the
// coupling check working on Component data.
type componentsPlugin struct {
	world      *Entities
	ids        uint32
	bodies     *Store[body]
	velocities *Store[velocity]
	colliders  *Store[collider]
}

func (p *componentsPlugin) Name() kernel.PluginName { return "components" }

func (p *componentsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *componentsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.bodies = RegisterComponent[body](registrar, p.world, p.ids)
	p.velocities = RegisterComponent[velocity](registrar, p.world, p.ids)
	p.colliders = RegisterComponent[collider](registrar, p.world, p.ids)
	return nil
}

// systemsPlugin subscribes Systems, and declares the components plugin because
// it locks the Stores that plugin owns.
type systemsPlugin struct {
	world     *Entities
	subscribe func(registrar *kernel.Registrar, world *Entities)
}

func (p *systemsPlugin) Name() kernel.PluginName { return "systems" }

func (p *systemsPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, "components"}
}

func (p *systemsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.subscribe(registrar, p.world)
	return nil
}

func move(q *Query[moveQuery]) {
	for _, it := range q.All() {
		it.Body.X += it.Velocity.X
		it.Body.Y += it.Velocity.Y
	}
}

func subscribeMove(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[moveSystem](ToHandler[app.UpdateEvent](world, move))
}

// world composes a real engine and runs it until the test ends, so every claim
// below is made against the kernel rather than against a stand-in for it.
func newWorld(t testing.TB, ids uint32, subscribe func(*kernel.Registrar, *Entities)) (
	*Entities, *componentsPlugin, *kernel.Engine,
) {
	t.Helper()
	entities := NewEntities(ids)
	components := &componentsPlugin{world: entities, ids: ids}
	engine := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(
			Plugin(entities),
			components,
			&systemsPlugin{world: entities, subscribe: subscribe},
		)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go engine.Run(ctx)
	<-engine.Ready()
	return entities, components, engine
}

// TestAComponentMoves is the tracer bullet: one System with a two-Component
// Query, on a real engine, driven by a real app.UpdateEvent. A write through a
// pointer field lands, and a read yields a copy.
func TestAComponentMoves(t *testing.T) {
	entities, components, engine := newWorld(t, 128, subscribeMove)

	moved := make([]Entity, 4)
	for i := range moved {
		e := entities.alloc()
		moved[i] = e
		components.bodies.Set(e, body{X: float32(i)})
		components.velocities.Set(e, velocity{X: 1, Y: 2})
	}

	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}

	for i, e := range moved {
		value, ok := components.bodies.Get(e)
		if !ok {
			t.Fatalf("entity %v lost its body", e)
		}
		if value.X != float32(i)+1 || value.Y != 2 {
			t.Fatalf("body of %v = %v after one tick, want {%v 2}", e, value, float32(i)+1)
		}
	}
}

// TestASystemMayNameTheEventItIsDrivenBy covers the one parameter besides a
// Query that this build accepts. It is legal and it is not the ordinary shape:
// a System that names an event can only ever be subscribed to that event, where
// the same gameplay should be drivable by a fixed-step tick or by a test
// harness publishing its own frames.
func TestASystemMayNameTheEventItIsDrivenBy(t *testing.T) {
	type tickSystem kernel.Subscription[app.UpdateEvent]

	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[tickSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], tick app.UpdateEvent) {
				dt := float32(tick.Dt)
				for _, it := range q.All() {
					it.Body.X += it.Velocity.X * dt
				}
			}))
	})
	e := entities.alloc()
	components.bodies.Set(e, body{})
	components.velocities.Set(e, velocity{X: 10})

	frame(t, engine, 0.5)
	frame(t, engine, 0.25)

	if value, _ := components.bodies.Get(e); value.X != 7.5 {
		t.Fatalf("body is %v after ticks of 0.5 and 0.25 at velocity 10, want {7.5 0}", value)
	}
}

// TestAQueryOnlyVisitsEntitiesHavingEveryComponent is the whole of what a Query
// selects on: presence, and nothing else.
func TestAQueryOnlyVisitsEntitiesHavingEveryComponent(t *testing.T) {
	entities, components, engine := newWorld(t, 128, subscribeMove)

	both, bodyOnly, velocityOnly := entities.alloc(), entities.alloc(), entities.alloc()
	components.bodies.Set(both, body{})
	components.velocities.Set(both, velocity{X: 1})
	components.bodies.Set(bodyOnly, body{})
	components.velocities.Set(velocityOnly, velocity{X: 1})

	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}

	if value, _ := components.bodies.Get(both); value.X != 1 {
		t.Fatalf("the entity with both Components was not visited: %v", value)
	}
	if value, _ := components.bodies.Get(bodyOnly); value.X != 0 {
		t.Fatalf("an entity with no velocity was visited: %v", value)
	}
}

// TestEveryHandlerTouchingAStoreReadsEntities holds the invariant the despawn
// traversal rests on: a Despawn reaches every Store through Go pointers the
// kernel does not police, and that is sound only because every handler that
// touches any Store declares read{*Entities}.
func TestEveryHandlerTouchingAStoreReadsEntities(t *testing.T) {
	_, _, engine := newWorld(t, 8, subscribeMove)

	description := engine.Describe()
	found := false
	for _, sub := range description.Subscriptions {
		if sub.Type != reflect.TypeFor[moveSystem]() {
			continue
		}
		found = true
		if !namesType(sub.Reads, "*ecs.Entities") {
			t.Fatalf("the move System reads %v, which does not include *ecs.Entities", sub.Reads)
		}
		if !namesType(sub.Reads, "velocity]") {
			t.Fatalf("the move System reads %v, which does not include the velocity Store", sub.Reads)
		}
		if !namesType(sub.Writes, "body]") {
			t.Fatalf("the move System writes %v, which does not include the body Store", sub.Writes)
		}
		if namesType(sub.Writes, "velocity]") {
			t.Fatalf("the move System write-locks the velocity Store, which it only reads")
		}
	}
	if !found {
		t.Fatalf("no moveSystem subscription in the description")
	}
}

// TestAQueryOverAnUnregisteredComponentFailsComposition names the Component and
// the Query, rather than the store type the user never wrote.
func TestAQueryOverAnUnregisteredComponentFailsComposition(t *testing.T) {
	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[guardSystem](ToHandler[app.UpdateEvent](world,
					func(q *Query[guardedQuery]) {}))
			}},
		)

	if failure == nil {
		t.Fatalf("composing a Query over an unregistered Component succeeded")
	}
	message := failure.Error()
	for _, want := range []string{"systems", "guardedQuery", "guarded"} {
		if !strings.Contains(message, want) {
			t.Fatalf("composition failure %q does not name %q", message, want)
		}
	}
}

type guarded struct{ Amount int32 }

type guardedQuery struct {
	Body    *body
	Guarded guarded
}

type guardSystem kernel.Subscription[app.UpdateEvent]

// TestASystemReturningAValueIsRejectedAtRegistration is a hard rule and not a
// style preference: reflect.Value.Call allocates for a callee that returns one.
func TestASystemReturningAValueIsRejectedAtRegistration(t *testing.T) {
	entities := NewEntities(8)
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System returning a value was accepted at registration")
		}
		if message, _ := recovered.(string); !strings.Contains(message, "returns") {
			t.Fatalf("panic %v does not say the System returns something", recovered)
		}
	}()
	ToHandler[app.UpdateEvent](entities, func(q *Query[moveQuery]) error { return nil })
}

// TestASystemTakingAnUnknownParameterIsRejected is the mistake every new user
// makes once, so the diagnostic matters more than the mechanism.
func TestASystemTakingAnUnknownParameterIsRejected(t *testing.T) {
	entities := NewEntities(8)
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System taking an unknown parameter was accepted at registration")
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, "*ecs.Store[") {
			t.Fatalf("panic %v does not name the offending parameter type", recovered)
		}
	}()
	ToHandler[app.UpdateEvent](entities, func(s *Store[body]) {})
}

func namesType(types []reflect.Type, want string) bool {
	for _, t := range types {
		if strings.Contains(t.String(), want) {
			return true
		}
	}
	return false
}
