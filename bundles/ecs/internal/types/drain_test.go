package types

import (
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// drainEvent is an app's own event, so that the drain System below is subscribed
// somewhere other than app.UpdateEvent: a drain is a call the app makes, and
// where it makes it is the app's.
type drainEvent struct{}

type (
	appDrainSystem  kernel.Subscription[drainEvent]
	tickDrainSystem kernel.Subscription[app.UpdateEvent]
)

// drainSystem is the whole of what an app writes to drain, and is the shape the
// spec gives: one parameter, one call.
func drainSystem(entities *WriteableEntities) { entities.Drain() }

// TestADrainWalksTheSpawnsThenTheDespawns is the order of one drain, and the
// order is the contract: the two registries are two separate walks, so every
// handle's spawns are applied before any handle's despawns however the handles
// were interleaved at registration, and each registry is walked in the order it
// was enrolled in.
func TestADrainWalksTheSpawnsThenTheDespawns(t *testing.T) {
	en := newEntities(8)
	var trace []string
	record := func(name string) func() { return func() { trace = append(trace, name) } }
	// Enrolled interleaved, which is what two Systems taking one handle each
	// produce, so a single registry walked once could not yield the want below.
	en.enrolDrainSpawn(record("spawn A"))
	en.enrolDrainDespawn(record("despawn A"))
	en.enrolDrainSpawn(record("spawn B"))
	en.enrolDrainDespawn(record("despawn B"))

	en.drain()

	want := []string{"spawn A", "spawn B", "despawn A", "despawn B"}
	if !slices.Equal(trace, want) {
		t.Fatalf("one drain walked %v, want %v", trace, want)
	}
}

// TestADrainOverEmptyBuffersWalksEveryEnrolledHandle is the absent
// empty-drain skip. A handle with nothing queued is still called, every drain,
// because a skip would be a branch guarding a branch: what a drain costs over
// an idle world is one length check per enrolled buffer, and that is the whole
// of the cost the design accepted.
func TestADrainOverEmptyBuffersWalksEveryEnrolledHandle(t *testing.T) {
	en := newEntities(8)
	spawns, despawns := 0, 0
	en.enrolDrainSpawn(func() { spawns++ })
	en.enrolDrainDespawn(func() { despawns++ })

	for range 3 {
		en.drain()
	}

	if spawns != 3 || despawns != 3 {
		t.Fatalf("three drains made %d spawn passes and %d despawn passes over an idle handle, want 3 and 3",
			spawns, despawns)
	}
}

// TestADrainOfAWorldThatDefersNothingChangesNothing is the registries empty:
// nothing is enrolled, so a drain touches neither the index space nor the free
// list, and the world an app that defers nothing runs in is the world it ran in
// before ecs.DrainOnUpdate existed.
func TestADrainOfAWorldThatDefersNothingChangesNothing(t *testing.T) {
	en := newEntities(8)
	var spawned []Entity
	for range 4 {
		spawned = append(spawned, en.alloc())
	}
	en.despawn(spawned[1])
	gens := slices.Clone(en.gens)
	free := slices.Clone(en.free)

	en.drain()

	if !slices.Equal(en.gens, gens) || !slices.Equal(en.free, free) {
		t.Fatalf("a drain of an undeferring world left generations %v and free list %v, want %v and %v",
			en.gens, en.free, gens, free)
	}
	for i, e := range spawned {
		if alive := en.Alive(e); alive != (i != 1) {
			t.Fatalf("%v is alive=%v after a drain, want %v", e, alive, i != 1)
		}
	}
}

// TestAnAppDrainSystemDrainsOnTheEventItIsSubscribedTo is the whole of what
// Drain is for: an app writes a drain System and schedules it where it belongs,
// and the drain happens on that event and on no other. One Drain drains
// everything queued in the Engine, whatever event queued it, so there is no
// per-event bookkeeping to get wrong — and a publication of an event nobody
// drains on drains nothing.
func TestAnAppDrainSystemDrainsOnTheEventItIsSubscribedTo(t *testing.T) {
	var drains atomic.Int64
	_, _, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		world, err := registrar.Dependency[*Entities]()
		if err != nil {
			t.Fatalf("the systems plugin cannot reach the authority: %v", err)
		}
		// Enrolment happens at registration, where a deferring handle's prepare
		// will do it, and the probe stands in for the handle's apply.
		world.enrolDrainSpawn(func() { drains.Add(1) })
		registrar.Subscribe[appDrainSystem](ToHandler[drainEvent](registrar, drainSystem))
	})
	executioner := engine.Executioner()

	// This package's authority stands in for the ecs plugin and subscribes no
	// drainer of its own, so an Update here drains nothing: what is measured is
	// the app's System and nothing else.
	frame(t, engine, 1)
	if got := drains.Load(); got != 0 {
		t.Fatalf("an Update nobody drains on made %d drains, want 0", got)
	}

	executioner.PublishEvent(drainEvent{}).Wait()
	if got := drains.Load(); got != 1 {
		t.Fatalf("one publication of the drain event made %d drains, want 1", got)
	}
	executioner.PublishEvent(drainEvent{}).Wait()
	if got := drains.Load(); got != 2 {
		t.Fatalf("two publications of the drain event made %d drains, want 2", got)
	}
}

// TestADrainSystemHoldsTheWideLockAndNothingBesides reads the lock set off the
// engine's description. Drain is a method on WriteableEntities and reachable
// nowhere else, so a drain always runs under write{*Entities}: it can never
// overlap a System writing a deferral buffer, because every System declares
// read{*Entities} and the scheduler refuses to run the two together. That
// exclusion is the whole reason the queue needs no lock of its own.
func TestADrainSystemHoldsTheWideLockAndNothingBesides(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		registrar.Subscribe[appDrainSystem](ToHandler[drainEvent](registrar, drainSystem))
	})

	entities := reflect.TypeFor[*Entities]()
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[appDrainSystem]() {
			continue
		}
		if !slices.Equal(subscription.Writes, []reflect.Type{entities}) ||
			len(subscription.Reads) != 0 || len(subscription.Uses) != 0 {
			t.Fatalf("a drain System writes %v, reads %v and uses %v; want write{*Entities} alone",
				subscription.Writes, subscription.Reads, subscription.Uses)
		}
		return
	}
	t.Fatalf("the architecture has no drain System")
}

// subscribeDrainOnUpdate is the general drainer as the ecs plugin subscribes
// it, spelled here because this package cannot import ecs's root: Last, on the
// Update, with two enrolled handles that have nothing queued.
func subscribeDrainOnUpdate(t testing.TB) func(*kernel.Registrar) {
	return func(registrar *kernel.Registrar) {
		world, err := registrar.Dependency[*Entities]()
		if err != nil {
			t.Fatalf("the systems plugin cannot reach the authority: %v", err)
		}
		world.enrolDrainSpawn(func() {})
		world.enrolDrainDespawn(func() {})
		registrar.Subscribe[tickDrainSystem](
			ToHandler[app.UpdateEvent](registrar, drainSystem)).Last()
	}
}

// TestADrainSitsOnTheEnginesAllocationLine measures the drained frame at two
// entity counts an order of magnitude apart. A drain that allocated would
// allocate whatever the population, so identical counts at 1k and 10k are the
// claim, and the 6-per-frame line is the engine's own.
//
// The second arm is what the general drainer actually costs an app, because the
// engine charges a frame for each subscription node it dispatches, whatever
// that node does: the drainer beside a Query is measured against two Queries,
// and the claim is that the drain node costs no more than any other System's.
func TestADrainSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const frames = 10_000
	measure := func(n int, subscribe func(*kernel.Registrar)) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		populate(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, so what is measured is steady
		// state and not the first tick.
		for range 100 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
			}
		})
		return float64(mallocs) / frames
	}
	drainer := subscribeDrainOnUpdate(t)
	beside := func(registrar *kernel.Registrar) {
		subscribeMove(registrar)
		drainer(registrar)
	}
	twoSystems := func(registrar *kernel.Registrar) {
		subscribeMove(registrar)
		registrar.Subscribe[tickDrainSystem](
			ToHandler[app.UpdateEvent](registrar, func(*Query[moveQuery]) {})).Last()
	}

	at1k, at10k := measure(1_000, drainer), measure(10_000, drainer)
	besideAQuery, twoOrdinary := measure(1_000, beside), measure(1_000, twoSystems)
	t.Logf("objects a frame: the drainer alone at 1k %.3f, at 10k %.3f; beside a Query %.3f, against two ordinary Systems %.3f",
		at1k, at10k, besideAQuery, twoOrdinary)

	if at10k > at1k+0.05 {
		t.Fatalf("a drained frame allocates with the entity count: %.3f a frame at 1k, %.3f at 10k", at1k, at10k)
	}
	if at10k > 6.5 {
		t.Fatalf("a drained frame costs %.3f objects, above the engine's 6-per-frame line", at10k)
	}
	if besideAQuery > twoOrdinary+0.05 {
		t.Fatalf("the drain node costs %.3f objects a frame against an ordinary System's %.3f",
			besideAQuery, twoOrdinary)
	}
}
