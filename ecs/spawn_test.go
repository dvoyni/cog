package ecs

import (
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// Structural change, and the barrier it takes. Nothing here is a Command: a
// Spawn is a handle the System already holds, so creating an Entity is a direct
// call and the exclusion was arranged before the frame started.

// spawnBundle is the Component set one act of creation makes. A Bundle field
// simply is a Component field: there is no conversion step anywhere.
type spawnBundle struct {
	Body     body
	Velocity velocity
}

type spawnSystem kernel.Subscription[app.UpdateEvent]

// TestASystemSpawns is the tracer bullet: a System whose signature names a
// Spawn creates an Entity carrying every Component the Bundle names, on a real
// engine, driven by a real app.UpdateEvent.
func TestASystemSpawns(t *testing.T) {
	entities, components, engine := newWorld(t, 128, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](world,
			func(sp *Spawn[spawnBundle]) {
				sp.New(spawnBundle{Body: body{X: 1, Y: 2}, Velocity: velocity{X: 3}})
			}))
	})

	frame(t, engine, 1)

	if components.bodies.Len() != 1 {
		t.Fatalf("one tick of a spawning System left %d bodies, want 1", components.bodies.Len())
	}
	if components.velocities.Len() != 1 {
		t.Fatalf("one tick of a spawning System left %d velocities, want 1", components.velocities.Len())
	}
	spawned := components.bodies.owners[0]
	if !entities.Alive(spawned) {
		t.Fatalf("the spawned entity %v is not Alive", spawned)
	}
	if value, ok := components.bodies.Get(spawned); !ok || value != (body{X: 1, Y: 2}) {
		t.Fatalf("the spawned entity's body is %v, %v; want {1 2}, true", value, ok)
	}
	if value, ok := components.velocities.Get(spawned); !ok || value != (velocity{X: 3}) {
		t.Fatalf("the spawned entity's velocity is %v, %v; want {3 0}, true", value, ok)
	}
}

type despawnSystem kernel.Subscription[app.UpdateEvent]

// TestASystemDespawns covers the other handle, and the reason there are two:
// this System never names a Bundle, because it never creates anything.
func TestASystemDespawns(t *testing.T) {
	var doomed Entity
	var reported, secondReport bool
	entities, components, engine := newWorld(t, 128, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[despawnSystem](ToHandler[app.UpdateEvent](world,
			func(we *WriteableEntities) {
				if !reported {
					reported = we.Despawn(doomed)
					return
				}
				secondReport = we.Despawn(doomed)
			}))
	})

	doomed, bystander := entities.alloc(), entities.alloc()
	components.bodies.Set(doomed, body{X: 1})
	components.velocities.Set(doomed, velocity{X: 1})
	components.colliders.Set(doomed, collider{Radius: 1})
	components.bodies.Set(bystander, body{X: 2})

	frame(t, engine, 1)

	if !reported {
		t.Fatalf("Despawn(%v) reported false for a live entity", doomed)
	}
	if entities.Alive(doomed) {
		t.Fatalf("%v is still Alive after a System despawned it", doomed)
	}
	// A despawn is total: every Store is emptied, naming no Component at all.
	for name, held := range map[string]bool{
		"body":     components.bodies.Has(doomed),
		"velocity": components.velocities.Has(doomed),
		"collider": components.colliders.Has(doomed),
	} {
		if held {
			t.Fatalf("the despawned entity still has its %s", name)
		}
	}
	if value, ok := components.bodies.Get(bystander); !ok || value.X != 2 {
		t.Fatalf("the bystander's body reads back as %v, %v; want {2 0}, true", value, ok)
	}

	frame(t, engine, 1)

	if secondReport {
		t.Fatalf("Despawn(%v) reported true the second time, want false for a retired handle", doomed)
	}
}

type churnSystem kernel.Subscription[app.UpdateEvent]

// TestPopulationHoldsSteadyAcrossHundredsOfTicks is the correctness half of the
// allocation claim: spawning and despawning the same count every tick must
// leave the population and the index space exactly where they started, because
// the free list recycles ids and the Store reuses the dense row.
func TestPopulationHoldsSteadyAcrossHundredsOfTicks(t *testing.T) {
	const perTick = 16
	const ticks = 300
	live := make([]Entity, 0, perTick)
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[churnSystem](ToHandler[app.UpdateEvent](world,
			func(sp *Spawn[spawnBundle], we *WriteableEntities) {
				for _, e := range live {
					if !we.Despawn(e) {
						t.Errorf("Despawn(%v) reported false for an entity spawned last tick", e)
					}
				}
				live = live[:0]
				for i := range perTick {
					live = append(live, sp.New(spawnBundle{Body: body{X: float32(i)}}))
				}
			}))
	})

	for range ticks {
		frame(t, engine, 1)
	}

	if components.bodies.Len() != perTick {
		t.Fatalf("%d ticks of %d spawns and %d despawns left %d bodies, want %d",
			ticks, perTick, perTick, components.bodies.Len(), perTick)
	}
	if used := len(entities.gens); used != perTick {
		t.Fatalf("%d ticks of churn used %d indices, want %d: the free list bounds the index space by peak population",
			ticks, used, perTick)
	}
	if grown := cap(components.bodies.dense); grown != 64 {
		t.Fatalf("the dense array is %d rows after %d ticks of churn, want the reserved 64: the Store reuses the row",
			grown, ticks)
	}
}

// TestSpawnWritesTheAuthorityAndOwnsItsComponents reads the declaration off the
// engine's own description rather than off the implementation. Two claims:
// write{*Entities} supersedes the read every System takes, and the per-Component
// write is still declared — redundant for locking, kept as the ownership
// declaration that makes the composition check fire.
func TestSpawnWritesTheAuthorityAndOwnsItsComponents(t *testing.T) {
	_, _, engine := newWorld(t, 16, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](world,
			func(sp *Spawn[spawnBundle]) { _ = sp }))
	})

	subscription := describeSubscription(t, engine, reflect.TypeFor[spawnSystem]())
	if !namesType(subscription.Writes, "*ecs.Entities") {
		t.Fatalf("a spawning System writes %v, which does not include *ecs.Entities", subscription.Writes)
	}
	if namesType(subscription.Reads, "*ecs.Entities") {
		t.Fatalf("a spawning System still reads *ecs.Entities: the write must supersede the read, not sit beside it")
	}
	for _, owned := range []string{"body]", "velocity]"} {
		if !namesType(subscription.Writes, owned) {
			t.Fatalf("a spawning System writes %v, which does not include the %s Store: the per-Component write is the ownership declaration",
				subscription.Writes, owned)
		}
	}
}

// TestDespawnNamesNoComponentAtAll is the shape that makes a data-driven spawn
// possible at all: Entities reaches every Store itself, so the barrier is one
// entry in the lock set and not N.
func TestDespawnNamesNoComponentAtAll(t *testing.T) {
	_, _, engine := newWorld(t, 16, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[despawnSystem](ToHandler[app.UpdateEvent](world,
			func(we *WriteableEntities) { _ = we }))
	})

	subscription := describeSubscription(t, engine, reflect.TypeFor[despawnSystem]())
	if !namesType(subscription.Writes, "*ecs.Entities") {
		t.Fatalf("a despawning System writes %v, which does not include *ecs.Entities", subscription.Writes)
	}
	if len(subscription.Writes) != 1 {
		t.Fatalf("a despawning System writes %v: a Despawn names no Component, so the barrier is one entry and not N",
			subscription.Writes)
	}
	if len(subscription.Reads) != 0 {
		t.Fatalf("a despawning System reads %v, want nothing: the write supersedes the read", subscription.Reads)
	}
}

// TestABundleOverAnUnregisteredComponentFailsComposition names the Component and
// the Bundle, rather than the store type the user never wrote.
func TestABundleOverAnUnregisteredComponentFailsComposition(t *testing.T) {
	type unregisteredBundle struct {
		Body    body
		Guarded guarded
	}
	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](world,
					func(sp *Spawn[unregisteredBundle]) {}))
			}},
		)

	if failure == nil {
		t.Fatalf("composing a Bundle over an unregistered Component succeeded")
	}
	for _, want := range []string{"systems", "unregisteredBundle", "guarded"} {
		if !strings.Contains(failure.Error(), want) {
			t.Fatalf("composition failure %q does not name %q", failure.Error(), want)
		}
	}
}

// TestABundleFieldIsAComponentValue: a Bundle field is not a Query field, and
// the mistake of copying one is worth a diagnostic of its own, because a spawn
// supplies the value rather than reaching one that already exists.
func TestABundleFieldIsAComponentValue(t *testing.T) {
	type pointerBundle struct{ Body *body }
	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](world,
					func(sp *Spawn[pointerBundle]) {}))
			}},
		)

	if failure == nil {
		t.Fatalf("a Bundle with a pointer field was accepted at composition")
	}
	for _, want := range []string{"pointerBundle", "Body"} {
		if !strings.Contains(failure.Error(), want) {
			t.Fatalf("composition failure %q does not name %q", failure.Error(), want)
		}
	}
}

// occupancy counts how many Systems are inside their bodies at once. It is the
// control the barrier claim needs: an observed occupancy of 1 proves nothing
// unless a 2 was observable on the same harness.
type occupancy struct {
	live atomic.Int32
	most atomic.Int32
}

// overlap enters, records the greatest occupancy seen, waits for company until
// the deadline, and leaves. Waiting rather than returning at once is what makes
// the 2 observable: two Systems that merely could run together might still be
// scheduled one after the other.
func (o *occupancy) overlap(wait time.Duration) {
	live := o.live.Add(1)
	for {
		most := o.most.Load()
		if live <= most || o.most.CompareAndSwap(most, live) {
			break
		}
	}
	for deadline := time.Now().Add(wait); o.live.Load() < 2 && time.Now().Before(deadline); {
		runtime.Gosched()
	}
	o.live.Add(-1)
}

type bodyQuery struct{ Body *body }

type colliderQuery struct{ Collider *collider }

type workerASystem kernel.Subscription[app.UpdateEvent]

type workerBSystem kernel.Subscription[app.UpdateEvent]

// TestWritingTheAuthorityExcludesEveryOtherSystem is the barrier measured rather
// than argued. Two Systems writing different Components run together; the same
// pair with the first one spawning does not, because write{*Entities}
// supersedes the read every System takes and therefore excludes all of them.
func TestWritingTheAuthorityExcludesEveryOtherSystem(t *testing.T) {
	const wait = 100 * time.Millisecond
	measure := func(spawning bool) int32 {
		seen := &occupancy{}
		// The two arms differ in exactly one thing: what the first System
		// declares. Both iterate the same Query and do the same work.
		var workerA any = func(q *Query[bodyQuery]) { seen.overlap(wait) }
		if spawning {
			workerA = func(q *Query[bodyQuery], sp *Spawn[spawnBundle]) { seen.overlap(wait) }
		}
		_, _, engine := newWorld(t, 16, func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[workerASystem](ToHandler[app.UpdateEvent](world, workerA))
			registrar.Subscribe[workerBSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[colliderQuery]) { seen.overlap(wait) }))
		})
		frame(t, engine, 1)
		return seen.most.Load()
	}

	if disjoint := measure(false); disjoint != 2 {
		t.Fatalf("two Systems writing different Components reached occupancy %d, want 2: "+
			"without an observable 2 the barrier's 1 proves nothing", disjoint)
	}
	if barrier := measure(true); barrier != 1 {
		t.Fatalf("the same pair with one of them spawning reached occupancy %d, want 1: "+
			"write{*Entities} is a total barrier", barrier)
	}
}

// TestATagIsAnOrdinaryBundleField is worth a test of its own because a
// zero-size field is where Go's padding rules bite: the Bundle's field offsets
// are what the spawn writes through, and a Tag carries nothing to write.
func TestATagIsAnOrdinaryBundleField(t *testing.T) {
	type taggedBundle struct {
		Body  body
		Solid solid
	}
	_, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](world,
			func(sp *Spawn[taggedBundle]) { sp.New(taggedBundle{Body: body{X: 7}}) }))
	})

	frame(t, engine, 1)

	if components.solids.Len() != 1 {
		t.Fatalf("the spawn left %d solids, want 1: a Tag is an ordinary Store and an ordinary Bundle field",
			components.solids.Len())
	}
	spawned := components.solids.owners[0]
	if value, ok := components.bodies.Get(spawned); !ok || value.X != 7 {
		t.Fatalf("the field beside the Tag reads back as %v, %v; want {7 0}, true", value, ok)
	}
}

type restructuringSystem kernel.Subscription[app.UpdateEvent]

// TestASystemMayRestructureTheEntityItIsVisiting is the guarantee the backwards
// walk exists to give, now that a System can reach a structural change at all.
// Two halves: despawning the current Entity skips nobody, and spawning one per
// visited Entity terminates — under a forward walk it would not, because the
// appended row is ahead of the cursor.
func TestASystemMayRestructureTheEntityItIsVisiting(t *testing.T) {
	const population = 200
	t.Run("despawning the current Entity", func(t *testing.T) {
		visited := 0
		entities, components, engine := newWorld(t, 4*population, func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[restructuringSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[moveQuery], we *WriteableEntities) {
					for e := range q.All() {
						visited++
						we.Despawn(e)
					}
				}))
		})
		populate(entities, components, population)

		frame(t, engine, 1)

		if visited != population {
			t.Fatalf("a System despawning as it went visited %d of %d Entities", visited, population)
		}
		if components.bodies.Len() != 0 {
			t.Fatalf("%d bodies survived a System that despawned every Entity it visited", components.bodies.Len())
		}
	})

	t.Run("spawning one Entity per visited Entity", func(t *testing.T) {
		visited := 0
		entities, components, engine := newWorld(t, 4*population, func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[restructuringSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[moveQuery], sp *Spawn[spawnBundle]) {
					for range q.All() {
						visited++
						sp.New(spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 1}})
					}
				}))
		})
		populate(entities, components, population)

		frame(t, engine, 1)

		if visited != population {
			t.Fatalf("a System spawning as it went visited %d Entities, want the %d it started with: "+
				"the walk reached Entities appended during the loop", visited, population)
		}
		if components.bodies.Len() != 2*population {
			t.Fatalf("the population is %d after one such tick, want %d", components.bodies.Len(), 2*population)
		}
	})
}

type handleSystem kernel.Subscription[app.UpdateEvent]

// handles runs one tick of a System that does nothing but keep its own
// parameters, so a test can call New and Despawn directly and count what they
// cost. Only a test in this package can hold them outside a frame; a System's
// only route to either is its signature.
func handles[B any](tb testing.TB, ids uint32) (*Spawn[B], *WriteableEntities, *componentsPlugin) {
	tb.Helper()
	var spawn *Spawn[B]
	var writeable *WriteableEntities
	_, components, engine := newWorld(tb, ids, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[handleSystem](ToHandler[app.UpdateEvent](world,
			func(sp *Spawn[B], we *WriteableEntities) { spawn, writeable = sp, we }))
	})
	frame(tb, engine, 1)
	return spawn, writeable, components
}

// newEscaping is the obvious spelling of New, and it is here as the control for
// the one the package ships. It takes the address of the bundle parameter and
// hands it to the same opaque per-field closures, which is what makes the bundle
// escape: the address of a parameter reaching a func value the compiler cannot
// see into is heap-allocated, once per spawn.
func (s *Spawn[B]) newEscaping(bundle B) Entity {
	e := s.entities.Get().alloc()
	buffer := unsafe.Pointer(&bundle)
	for i := range s.fields {
		field := &s.fields[i]
		field.set(e, unsafe.Add(buffer, field.offset))
	}
	return e
}

// TestSpawnStagesItsBundleThroughAField is the trap an implementation hits, made
// a test rather than a comment: the two spellings differ only in where the
// bundle lives, and one of them allocates per spawn.
func TestSpawnStagesItsBundleThroughAField(t *testing.T) {
	spawn, writeable, _ := handles[spawnBundle](t, 64)
	round := func(create func(spawnBundle) Entity) float64 {
		bundle := spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 2}}
		// Warm the free list and the dense rows, so what is measured is the
		// steady state a game runs in and not the first growth.
		for range 64 {
			writeable.Despawn(create(bundle))
		}
		return testing.AllocsPerRun(100, func() { writeable.Despawn(create(bundle)) })
	}

	staged := round(spawn.New)
	escaping := round(spawn.newEscaping)
	t.Logf("objects a spawn-and-despawn: staged through a field %.2f, through the parameter's address %.2f",
		staged, escaping)

	if staged != 0 {
		t.Fatalf("a spawn and despawn allocated %v objects, want 0", staged)
	}
	if escaping == 0 {
		t.Fatalf("the escaping spelling allocated nothing, so this test no longer controls anything: "+
			"staged %v, escaping %v", staged, escaping)
	}
}

func describeSubscription(t testing.TB, engine *kernel.Engine, want reflect.Type) kernel.SubscriptionDescription {
	t.Helper()
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type == want {
			return subscription
		}
	}
	t.Fatalf("no %v subscription in the description", want)
	return kernel.SubscriptionDescription{}
}
