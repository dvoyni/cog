package internal

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of deferred.md's deferring despawn handle. Each System
// here is invoked as a command, so a test decides exactly which System runs
// when — the queuer's run, then the drain's, then an observer's — which is what
// makes "before the drain" and "after the drain" statements about an order
// rather than about a schedule.

type (
	deferredRequest  struct{}
	deferredResponse struct{}
)

type (
	deferredQueueCmd kernel.Command[deferredRequest, deferredResponse]
	deferredPairCmd  kernel.Command[deferredRequest, deferredResponse]
	deferredBothCmd  kernel.Command[deferredRequest, deferredResponse]
	deferredDrainCmd kernel.Command[deferredRequest, deferredResponse]
	deferredReadCmd  kernel.Command[deferredRequest, deferredResponse]
)

// queuing is what an ordinary deferring System holds: the handle, a Query it
// iterates, and an accessor it probes with. The two readers are there so that
// what the queuer can see of its own queue is asked through the two routes an
// author has.
type queuing struct {
	despawn *DeferredDespawn
	q       *Query[typesMoveQuery]
	bodies  *Get[body]
}

// deferredWorld is a world of deferring Systems, a drain System and a Hooks
// reader of collider, each a command.
type deferredWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	queue      func(it queuing)
	pair       func(first, second *DeferredDespawn)
	both       func(we *WriteableEntities, despawn *DeferredDespawn)
	// despawned is what the reader heard since the last read, which is how "and
	// records nothing" is asked.
	despawned []heard
}

func newDeferredWorld(t *testing.T) *deferredWorld {
	t.Helper()
	w := &deferredWorld{}
	w.entities, w.components, w.engine = newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[deferredQueueCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			func(despawn *DeferredDespawn, q *Query[typesMoveQuery], bodies *Get[body]) {
				w.queue(queuing{despawn, q, bodies})
			}))
		registrar.HandleCommand[deferredPairCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			func(first *DeferredDespawn, second *DeferredDespawn) { w.pair(first, second) }))
		registrar.HandleCommand[deferredBothCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			func(we *WriteableEntities, despawn *DeferredDespawn) { w.both(we, despawn) }))
		registrar.HandleCommand[deferredDrainCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			typesDrainSystem))
		registrar.HandleCommand[deferredReadCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			func(h *Hooks[collider, HookDespawned]) {
				w.despawned = w.despawned[:0]
				listen(h, &w.despawned, radius)
			}))
	})
	return w
}

// populated gives n Entities a body, a velocity and a collider, so each is
// iterated by moveQuery, reached by Get[body] and recorded by the collider
// reader when it is despawned.
func (w *deferredWorld) populated(n int) []Entity {
	made := make([]Entity, n)
	for i := range made {
		e := w.entities.alloc()
		w.components.bodies.Set(e, body{X: float32(i)})
		w.components.velocities.Set(e, typesVelocity{})
		w.components.colliders.Set(e, collider{Radius: float32(i) + 1})
		made[i] = e
	}
	return made
}

func (w *deferredWorld) queued(t *testing.T, queue func(it queuing)) {
	t.Helper()
	w.queue = queue
	w.engine.Executioner().ExecuteCommand[deferredQueueCmd](deferredRequest{})
}

func (w *deferredWorld) paired(t *testing.T, pair func(first, second *DeferredDespawn)) {
	t.Helper()
	w.pair = pair
	w.engine.Executioner().ExecuteCommand[deferredPairCmd](deferredRequest{})
}

func (w *deferredWorld) composed(t *testing.T, both func(we *WriteableEntities, despawn *DeferredDespawn)) {
	t.Helper()
	w.both = both
	w.engine.Executioner().ExecuteCommand[deferredBothCmd](deferredRequest{})
}

func (w *deferredWorld) drained(t *testing.T) {
	t.Helper()
	w.engine.Executioner().ExecuteCommand[deferredDrainCmd](deferredRequest{})
}

func (w *deferredWorld) read(t *testing.T) {
	t.Helper()
	w.engine.Executioner().ExecuteCommand[deferredReadCmd](deferredRequest{})
}

// seen is every Entity the Query yields, in the order it yields them.
func seen(q *Query[typesMoveQuery]) []Entity {
	var visited []Entity
	for e := range q.All() {
		visited = append(visited, e)
	}
	return visited
}

// TestADeferringHandleHasOneExportedMethod is the whole of the handle's surface.
// There is no Len, no Pending, no Clear and no Drain: nothing reports what is
// queued, capacity belongs to ShrinkCmd, and Drain stays on WriteableEntities so
// that a System which drains names the wide lock in its signature.
//
// Despawn returns nothing, which is the house rule applied rather than an
// exception to it: an operation that can miss reports bool, one that cannot
// returns nothing, and a queued Despawn cannot miss at the call because the
// drain decides.
func TestADeferringHandleHasOneExportedMethod(t *testing.T) {
	handle := reflect.TypeFor[*DeferredDespawn]()
	if handle.NumMethod() != 1 {
		names := make([]string, handle.NumMethod())
		for i := range names {
			names[i] = handle.Method(i).Name
		}
		t.Fatalf("*DeferredDespawn exports %v, want Despawn alone", names)
	}
	method := handle.Method(0)
	if method.Name != "Despawn" {
		t.Fatalf("*DeferredDespawn's one method is %s, want Despawn", method.Name)
	}
	if method.Type.NumIn() != 2 || method.Type.In(1) != reflect.TypeFor[Entity]() {
		t.Fatalf("Despawn takes %v, want one Entity", method.Type)
	}
	if method.Type.NumOut() != 0 {
		t.Fatalf("Despawn returns %d value(s), want none: a queued Despawn cannot miss at the call",
			method.Type.NumOut())
	}
}

type deferringSystem kernel.Subscription[app.UpdateEvent]

// TestADeferringSystemDeclaresTheReadAndNothingBesides is what the whole design
// buys, read off the engine's own description: a System that retires Entities
// holds read{*Entities} rather than the write, so it runs beside every Query and
// every accessor and is excluded only against writers.
//
// The read is declared by the handle itself and not merely inherited from the
// one every System takes: a System holding only this handle would otherwise name
// nothing on the authority and could append to its buffer while the drain walked
// it. The kernel rather than a comment keeps the two apart.
func TestADeferringSystemDeclaresTheReadAndNothingBesides(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		registrar.Subscribe[deferringSystem](ToHandler[app.UpdateEvent](registrar,
			func(despawn *DeferredDespawn) {}))
	})

	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[deferringSystem]() {
			continue
		}
		if !slices.Equal(subscription.Reads, []reflect.Type{entitiesType}) ||
			len(subscription.Writes) != 0 || len(subscription.Uses) != 0 {
			t.Fatalf("a deferring System reads %v, writes %v and uses %v; want read{*Entities} alone",
				subscription.Reads, subscription.Writes, subscription.Uses)
		}
		return
	}
	t.Fatalf("the architecture has no deferring System")
}

// TestAQueuedDespawnIsInvisibleUntilTheDrain is the visibility rule: queuing is
// not a Structural change; the drain is. Before the drain the Entity is alive by
// every route there is — Alive, an accessor, a Query — and after it every System
// ordered after the drain System sees it gone.
func TestAQueuedDespawnIsInvisibleUntilTheDrain(t *testing.T) {
	w := newDeferredWorld(t)
	made := w.populated(3)
	doomed := made[1]

	w.queued(t, func(it queuing) { it.despawn.Despawn(doomed) })

	if !w.entities.Alive(doomed) {
		t.Fatalf("%v is not alive after its Despawn was queued", doomed)
	}
	var visited []Entity
	var reached bool
	w.queued(t, func(it queuing) {
		visited = seen(it.q)
		_, reached = it.bodies.Of(doomed)
	})
	if !slices.Contains(visited, doomed) {
		t.Fatalf("a Query visited %v before the drain, which does not include the queued %v", visited, doomed)
	}
	if !reached {
		t.Fatalf("an accessor did not reach the queued %v before the drain", doomed)
	}

	w.drained(t)

	if w.entities.Alive(doomed) {
		t.Fatalf("%v is still alive after the drain", doomed)
	}
	w.queued(t, func(it queuing) {
		visited = seen(it.q)
		_, reached = it.bodies.Of(doomed)
	})
	if slices.Contains(visited, doomed) {
		t.Fatalf("a Query visited %v after the drain, which still includes the drained %v", visited, doomed)
	}
	if reached {
		t.Fatalf("an accessor still reached %v after the drain", doomed)
	}
	for _, e := range []Entity{made[0], made[2]} {
		if !w.entities.Alive(e) {
			t.Fatalf("the drain took %v with it, which nothing queued", e)
		}
	}
}

// TestTheQueuerSeesNothingOfItsOwnQueue is the same rule turned on the System
// that made the queue: having queued a Despawn of X it still reaches X and still
// iterates it for the rest of its run. There is no per-run filter on Queries — a
// filter would be a read on the hot path for the minority of Systems that defer
// — so a System that must skip X keeps its own local note.
func TestTheQueuerSeesNothingOfItsOwnQueue(t *testing.T) {
	w := newDeferredWorld(t)
	made := w.populated(3)
	doomed := made[1]

	var visited []Entity
	var reached bool
	w.queued(t, func(it queuing) {
		it.despawn.Despawn(doomed)
		visited = seen(it.q)
		_, reached = it.bodies.Of(doomed)
	})

	if !slices.Contains(visited, doomed) {
		t.Fatalf("the queuer's own Query visited %v, which does not include the %v it had just queued",
			visited, doomed)
	}
	if !reached {
		t.Fatalf("the queuer's own accessor did not reach the %v it had just queued", doomed)
	}
}

// TestAQueuedDespawnOfAnEntityThatIsNotAliveDoesNothing is the one rule that
// covers every way a queued Despawn can arrive at a drain with nothing to do.
// There are no sub-cases and no distinction between a handle stale by a tick and
// one stale by an hour: the drain applies what it can and drops what it cannot,
// and it records nothing for what it drops. Nothing panics, in a validating
// build or any other — two Systems that both decide an Entity is finished
// produce exactly this, and the generation check is the designed answer.
func TestAQueuedDespawnOfAnEntityThatIsNotAliveDoesNothing(t *testing.T) {
	for _, probe := range []struct {
		name string
		// act queues the doomed Entity however this case reaches the drain with
		// it already dead, and leaves the drain to the caller.
		act func(t *testing.T, w *deferredWorld, doomed Entity)
	}{
		{"despawned immediately earlier in the frame", func(t *testing.T, w *deferredWorld, doomed Entity) {
			w.queued(t, func(it queuing) { it.despawn.Despawn(doomed) })
			w.composed(t, func(we *WriteableEntities, _ *DeferredDespawn) { we.Despawn(doomed) })
		}},
		{"despawned by another handle earlier in the same drain", func(t *testing.T, w *deferredWorld, doomed Entity) {
			w.paired(t, func(first, second *DeferredDespawn) {
				first.Despawn(doomed)
				second.Despawn(doomed)
			})
		}},
		{"queued twice by the same handle", func(t *testing.T, w *deferredWorld, doomed Entity) {
			w.queued(t, func(it queuing) {
				it.despawn.Despawn(doomed)
				it.despawn.Despawn(doomed)
			})
		}},
		{"held from an earlier frame", func(t *testing.T, w *deferredWorld, doomed Entity) {
			w.queued(t, func(it queuing) { it.despawn.Despawn(doomed) })
			w.drained(t)
			w.queued(t, func(it queuing) { it.despawn.Despawn(doomed) })
		}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			w := newDeferredWorld(t)
			made := w.populated(3)
			doomed := made[1]

			probe.act(t, w, doomed)
			w.drained(t)
			w.read(t)

			expectHeard(t, "the Despawns one drain recorded", w.despawned,
				[]heard{{doomed, "despawned+removed", 2}})
			for _, e := range []Entity{made[0], made[2]} {
				if !w.entities.Alive(e) {
					t.Fatalf("the drain took %v with it, which nothing queued", e)
				}
			}

			// A second drain over the same buffers is the same rule again, and is
			// what shows the drain empties them: nothing more happens and nothing
			// more is recorded.
			w.drained(t)
			w.read(t)
			expectHeard(t, "the Despawns a second drain recorded", w.despawned, nil)
		})
	}
}

// TestAStaleQueuedDespawnNeverReachesTheEntityThatRecycledItsIndex is what makes
// queuing a Despawn safe at all, and it is stated as its own test rather than
// left to be inferred from the id layout. A handle whose index has been recycled
// into a different Entity carries the old generation, so it fails Alive and the
// drain drops it: the Entity now holding that index is untouched.
func TestAStaleQueuedDespawnNeverReachesTheEntityThatRecycledItsIndex(t *testing.T) {
	w := newDeferredWorld(t)
	doomed := w.populated(1)[0]

	w.queued(t, func(it queuing) { it.despawn.Despawn(doomed) })
	// Despawned immediately, so the index is back on the free list with the queue
	// still holding the handle that named it.
	w.composed(t, func(we *WriteableEntities, _ *DeferredDespawn) { we.Despawn(doomed) })
	recycled := w.entities.alloc()
	w.components.bodies.Set(recycled, body{X: 9})
	w.components.velocities.Set(recycled, typesVelocity{})
	w.components.colliders.Set(recycled, collider{Radius: 9})
	if recycled.idx() != doomed.idx() {
		t.Fatalf("%v did not recycle %v's index, so this test is not testing what it says", recycled, doomed)
	}

	w.drained(t)
	w.read(t)

	if !w.entities.Alive(recycled) {
		t.Fatalf("a stale queued Despawn of %v despawned %v, which recycled its index", doomed, recycled)
	}
	if _, ok := w.components.colliders.Get(recycled); !ok {
		t.Fatalf("a stale queued Despawn emptied %v's Stores", recycled)
	}
	expectHeard(t, "the Despawns recorded around a recycled index", w.despawned,
		[]heard{{doomed, "despawned+removed", 1}})
}

// TestAQueueSurvivesASystemInvokedTwiceBeforeADrain is why nothing empties a
// buffer but a drain. resolve runs once per invocation, not once a tick, so a
// handle that reset its buffer there would silently lose the first run's queue
// whenever a System was invoked twice before a drain — which is what a System
// subscribed to an event published twice in a frame is.
func TestAQueueSurvivesASystemInvokedTwiceBeforeADrain(t *testing.T) {
	w := newDeferredWorld(t)
	made := w.populated(3)

	w.queued(t, func(it queuing) { it.despawn.Despawn(made[0]) })
	w.queued(t, func(it queuing) { it.despawn.Despawn(made[2]) })
	w.drained(t)

	for _, e := range []Entity{made[0], made[2]} {
		if w.entities.Alive(e) {
			t.Fatalf("%v survived the drain: the queue of one of the two runs was lost", e)
		}
	}
	if !w.entities.Alive(made[1]) {
		t.Fatalf("the drain took %v with it, which nothing queued", made[1])
	}
}

type composedSystem kernel.Subscription[app.UpdateEvent]

// TestAWriteAndADeferringHandleInOneSystemCompose is the half-migrated System,
// and it is legal, silent and checked nowhere: a registration panic would be the
// ECS telling an author their System is pointless rather than wrong. The write
// subsumes the read, so the System holds the barrier for its whole run and the
// queue buys it nothing — its queued despawns simply land a drain later than its
// immediate ones.
func TestAWriteAndADeferringHandleInOneSystemCompose(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		registrar.Subscribe[composedSystem](ToHandler[app.UpdateEvent](registrar,
			func(we *WriteableEntities, despawn *DeferredDespawn) {}))
	})

	found := false
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[composedSystem]() {
			continue
		}
		found = true
		if !slices.Equal(subscription.Writes, []reflect.Type{entitiesType}) || len(subscription.Reads) != 0 {
			t.Fatalf("a System holding both handles writes %v and reads %v; want write{*Entities} alone",
				subscription.Writes, subscription.Reads)
		}
	}
	if !found {
		t.Fatalf("a System holding both handles did not compose")
	}

	w := newDeferredWorld(t)
	made := w.populated(2)
	w.composed(t, func(we *WriteableEntities, despawn *DeferredDespawn) {
		we.Despawn(made[0])
		despawn.Despawn(made[1])
	})

	if w.entities.Alive(made[0]) {
		t.Fatalf("an immediate Despawn beside a queued one did not land at the call")
	}
	if !w.entities.Alive(made[1]) {
		t.Fatalf("a queued Despawn beside an immediate one landed at the call")
	}
	w.drained(t)
	if w.entities.Alive(made[1]) {
		t.Fatalf("the queued Despawn of a System that also holds the write did not land at the drain")
	}
}

// TestTheDrainEmptiesTheBufferAndKeepsItsCapacity is the steady-state claim, and
// ShrinkCmd is how the capacity comes back. A deferral buffer is Scratch — the
// same area a Query's walk and a writer's row copies are — so KeepScratch opts
// it out and ShrinkResponse.Scratch counts its bytes, with no new Keep flag and
// no new area.
func TestTheDrainEmptiesTheBufferAndKeepsItsCapacity(t *testing.T) {
	const queued = 32
	var handle *DeferredDespawn
	entities, _, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		// One deferring System and one drain System, and neither holds a Query or
		// a row copy, so every Scratch byte the shrink reports is the buffer's.
		registrar.HandleCommand[deferredQueueCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			func(despawn *DeferredDespawn) { handle = despawn }))
		registrar.HandleCommand[deferredDrainCmd](ToExecute[deferredRequest, deferredResponse](registrar,
			typesDrainSystem))
	})
	executioner := engine.Executioner()

	made := make([]Entity, queued)
	for i := range made {
		made[i] = entities.alloc()
	}
	executioner.ExecuteCommand[deferredQueueCmd](deferredRequest{})
	for _, e := range made {
		handle.Despawn(e)
	}
	if len(handle.queued) != queued {
		t.Fatalf("a handle queued %d Despawns and holds %d", queued, len(handle.queued))
	}

	executioner.ExecuteCommand[deferredDrainCmd](deferredRequest{})

	if len(handle.queued) != 0 {
		t.Fatalf("a drained buffer holds %d Entities, want none", len(handle.queued))
	}
	want := uintptr(queued) * unsafe.Sizeof(Entity(0))
	if uintptr(cap(handle.queued))*unsafe.Sizeof(Entity(0)) < want {
		t.Fatalf("a drained buffer kept capacity for %d Entities, want at least %d",
			cap(handle.queued), queued)
	}

	kept := executioner.ExecuteCommand[shrinkCmd](ShrinkRequest{KeepScratch: true})
	if kept.Scratch != 0 {
		t.Fatalf("KeepScratch released %d bytes of deferral buffer, want 0", kept.Scratch)
	}
	if cap(handle.queued) == 0 {
		t.Fatalf("KeepScratch gave the deferral buffer back anyway")
	}
	released := executioner.ExecuteCommand[shrinkCmd](ShrinkRequest{})
	if released.Scratch < want {
		t.Fatalf("a shrink reported %d Scratch bytes for a buffer of %d Entities, want at least %d",
			released.Scratch, queued, want)
	}
	if cap(handle.queued) != 0 {
		t.Fatalf("a shrink left %d Entities of capacity in the deferral buffer", cap(handle.queued))
	}
}

type (
	deferQueueSystem kernel.Subscription[app.UpdateEvent]
	deferDrainSystem kernel.Subscription[app.UpdateEvent]
	deferWatchSystem kernel.Subscription[app.UpdateEvent]
)

// TestAnAppDrainSystemMakesTheChangeVisibleWithinThePublication is why app drain
// Systems exist, and it falls straight out of the visibility rule rather than
// being special-cased: the System that made the change is the drain System, so
// whatever is ordered after it sees the change on that same publication.
func TestAnAppDrainSystemMakesTheChangeVisibleWithinThePublication(t *testing.T) {
	var doomed Entity
	var before, after []Entity
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.Subscribe[deferQueueSystem](ToHandler[app.UpdateEvent](registrar,
			func(despawn *DeferredDespawn, q *Query[typesMoveQuery]) {
				before = seen(q)
				despawn.Despawn(doomed)
			}))
		registrar.Subscribe[deferDrainSystem](ToHandler[app.UpdateEvent](registrar,
			typesDrainSystem)).After[deferQueueSystem]()
		registrar.Subscribe[deferWatchSystem](ToHandler[app.UpdateEvent](registrar,
			func(q *Query[typesMoveQuery]) { after = seen(q) })).After[deferDrainSystem]()
	})

	made := make([]Entity, 3)
	for i := range made {
		made[i] = entities.alloc()
		components.bodies.Set(made[i], body{})
		components.velocities.Set(made[i], typesVelocity{})
	}
	doomed = made[1]

	frame(t, engine, 1)

	if !slices.Contains(before, doomed) {
		t.Fatalf("the queuer visited %v, which does not include the %v it queued", before, doomed)
	}
	if slices.Contains(after, doomed) {
		t.Fatalf("a System ordered after the drain visited %v, which still includes the drained %v",
			after, doomed)
	}
	if len(after) != len(before)-1 {
		t.Fatalf("a System ordered after the drain visited %v, want the %v it queued gone and nothing else",
			after, doomed)
	}
}

type deferCycleSystem kernel.Subscription[app.UpdateEvent]

// deferringCycle spawns one Entity, queues a deferred Despawn of one and drains,
// so the population is what it was and the buffer is appended to and emptied
// every frame. immediateCycle is the same System with the Despawn made at the
// call, and is what the deferring arm is read against.
//
// The two are written out rather than branched on a captured flag, because a
// System func that closes over one costs three objects a frame through
// reflect.Value.Call — a measurement artefact that would swamp what is measured
// here.
func deferringCycle(spawn *Spawn[spawnSet], we *WriteableEntities, despawn *DeferredDespawn, q *Query[typesMoveQuery]) {
	for e := range q.All() {
		despawn.Despawn(e)
		break
	}
	we.Drain()
	spawn.New(spawnSet{Velocity: typesVelocity{X: 1}})
}

func immediateCycle(spawn *Spawn[spawnSet], we *WriteableEntities, despawn *DeferredDespawn, q *Query[typesMoveQuery]) {
	for e := range q.All() {
		we.Despawn(e)
		break
	}
	we.Drain()
	spawn.New(spawnSet{Velocity: typesVelocity{X: 1}})
}

// TestADeferredDespawnSitsOnTheEnginesAllocationLine measures a steady state
// rather than an idle world: every frame spawns one Entity, queues a deferred
// Despawn of one, and drains it, so the population is what it was and the buffer
// is appended to and emptied every frame. A buffer that grew, or a drain that
// rebuilt one, would allocate whatever the population, so identical counts at 1k
// and 10k are the claim and the 6-per-frame line is the engine's own.
//
// The whole cycle is one System, which is the composed case the spec allows and
// calls honest: the write subsumes the read, the System holds the barrier for
// its whole run, and the queue buys it nothing. It is measured this way because
// the engine charges a frame for each subscription node it dispatches, whatever
// the node does — the drain test next door measures that charge — so the number
// this test is about is only legible with one node.
func TestADeferredDespawnSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const frames = 10_000
	cycle := func(system any) func(*kernel.Registrar) {
		return func(registrar *kernel.Registrar) {
			registrar.Subscribe[deferCycleSystem](ToHandler[app.UpdateEvent](registrar, system))
		}
	}
	measure := func(n int, subscribe func(*kernel.Registrar)) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		populate(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, the deferral buffer included, so
		// what is measured is steady state and not the first tick.
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

	at1k, at10k := measure(1_000, cycle(deferringCycle)), measure(10_000, cycle(deferringCycle))
	immediate := measure(1_000, cycle(immediateCycle))
	t.Logf("objects a frame, one node spawning, despawning and draining: deferred at 1k %.3f, at 10k %.3f; immediate at 1k %.3f",
		at1k, at10k, immediate)

	if at10k > at1k+0.05 {
		t.Fatalf("a deferring frame allocates with the entity count: %.3f a frame at 1k, %.3f at 10k", at1k, at10k)
	}
	if at10k > 6.5 {
		t.Fatalf("a deferring frame costs %.3f objects, above the engine's 6-per-frame line", at10k)
	}
	if at1k > immediate+0.05 {
		t.Fatalf("deferring a Despawn costs %.3f objects a frame against the immediate %.3f", at1k, immediate)
	}
}

// TestTheRefusalNamesTheDeferringHandle keeps the hand-maintained list of legal
// System parameters honest: the sentence a bad signature gets is where an author
// finds out the handle exists.
func TestTheRefusalNamesTheDeferringHandle(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System taking an unknown parameter was accepted at registration")
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, "*ecs.DeferredDespawn") {
			t.Fatalf("the refusal %q does not name *ecs.DeferredDespawn", message)
		}
	}()
	ToHandler[app.UpdateEvent](nil, func(s *Store[body]) {})
}
