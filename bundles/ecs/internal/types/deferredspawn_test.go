package types

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of deferred.md's deferring spawn handle and the Reserved
// Entity it hands back. Each System here is invoked as a command, so a test
// decides exactly which System runs when — the queuer's run, then the drain's,
// then an observer's — which is what makes "before the drain" and "after the
// drain" statements about an order rather than about a schedule.

// reservedSet is the Component set a deferred New queues. It carries collider
// so a Hooks reader hears what the drain records, and homing so a Reserved
// Entity can be stored as a Reference in something queued beside it — which is
// what a Reserved Entity is for.
type reservedSet struct {
	Body     body
	Collider collider
	Homing   homing
}

// reservedQuery is the one-Component Query the tests watch the population
// through, because reservedSet carries no velocity and moveQuery would never
// yield what a deferred New queued.
type reservedQuery struct {
	Body *body
}

type (
	reservedRequest  struct{}
	reservedResponse struct{}
)

type (
	reservedQueueCmd  kernel.Command[reservedRequest, reservedResponse]
	reservedPairCmd   kernel.Command[reservedRequest, reservedResponse]
	reservedLateCmd   kernel.Command[reservedRequest, reservedResponse]
	reservedBothCmd   kernel.Command[reservedRequest, reservedResponse]
	reservedCycleCmd  kernel.Command[reservedRequest, reservedResponse]
	reservedProbeCmd  kernel.Command[reservedRequest, reservedResponse]
	reservedDrainCmd  kernel.Command[reservedRequest, reservedResponse]
	reservedRecordCmd kernel.Command[reservedRequest, reservedResponse]
)

// probing is every immediate route to an Entity an author has, in one System:
// the authority, the two writers and the two readers. It is what "a Reserved
// Entity cannot be touched immediately" is asked through.
type probing struct {
	we     *WriteableEntities
	set    *Set[body]
	remove *Remove[body]
	bodies *Get[body]
	q      *Query[reservedQuery]
}

// reservedWorld is a world of deferring spawn Systems, a drain System and a
// Hooks reader of collider, each a command.
type reservedWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	queue      func(spawn *DeferredSpawn[reservedSet])
	pair       func(first, second *DeferredSpawn[reservedSet])
	late       func(spawn *DeferredSpawn[reservedSet])
	both       func(immediate *Spawn[reservedSet], deferred *DeferredSpawn[reservedSet])
	cycle      func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn)
	probe      func(it probing)
	// recorded is what the reader heard since the last read, which is how "and
	// records both" is asked.
	recorded []heard
}

func newReservedWorld(t *testing.T) *reservedWorld {
	t.Helper()
	w := &reservedWorld{}
	w.entities, w.components, w.engine = newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[reservedQueueCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(spawn *DeferredSpawn[reservedSet]) { w.queue(spawn) }))
		registrar.HandleCommand[reservedPairCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(first *DeferredSpawn[reservedSet], second *DeferredSpawn[reservedSet]) { w.pair(first, second) }))
		// Registered after the pair, so that what it queues is applied after what
		// the pair queued however the two are called: enrolment order between
		// Systems is registration order.
		registrar.HandleCommand[reservedLateCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(spawn *DeferredSpawn[reservedSet]) { w.late(spawn) }))
		registrar.HandleCommand[reservedBothCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(immediate *Spawn[reservedSet], deferred *DeferredSpawn[reservedSet]) { w.both(immediate, deferred) }))
		registrar.HandleCommand[reservedCycleCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) { w.cycle(spawn, despawn) }))
		registrar.HandleCommand[reservedProbeCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(we *WriteableEntities, set *Set[body], remove *Remove[body], bodies *Get[body],
				q *Query[reservedQuery],
			) {
				w.probe(probing{we, set, remove, bodies, q})
			}))
		registrar.HandleCommand[reservedDrainCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			drainSystem))
		registrar.HandleCommand[reservedRecordCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(h *Hooks[collider, HookSpawnedDespawned]) {
				w.recorded = w.recorded[:0]
				listen(h, &w.recorded, radius)
			}))
	})
	return w
}

func (w *reservedWorld) queued(t *testing.T, queue func(spawn *DeferredSpawn[reservedSet])) {
	t.Helper()
	w.queue = queue
	w.engine.Executioner().ExecuteCommand[reservedQueueCmd](reservedRequest{})
}

func (w *reservedWorld) paired(t *testing.T, pair func(first, second *DeferredSpawn[reservedSet])) {
	t.Helper()
	w.pair = pair
	w.engine.Executioner().ExecuteCommand[reservedPairCmd](reservedRequest{})
}

func (w *reservedWorld) lately(t *testing.T, late func(spawn *DeferredSpawn[reservedSet])) {
	t.Helper()
	w.late = late
	w.engine.Executioner().ExecuteCommand[reservedLateCmd](reservedRequest{})
}

func (w *reservedWorld) composed(t *testing.T, both func(immediate *Spawn[reservedSet], deferred *DeferredSpawn[reservedSet])) {
	t.Helper()
	w.both = both
	w.engine.Executioner().ExecuteCommand[reservedBothCmd](reservedRequest{})
}

func (w *reservedWorld) cycled(t *testing.T, cycle func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn)) {
	t.Helper()
	w.cycle = cycle
	w.engine.Executioner().ExecuteCommand[reservedCycleCmd](reservedRequest{})
}

func (w *reservedWorld) probed(t *testing.T, probe func(it probing)) {
	t.Helper()
	w.probe = probe
	w.engine.Executioner().ExecuteCommand[reservedProbeCmd](reservedRequest{})
}

func (w *reservedWorld) drained(t *testing.T) {
	t.Helper()
	w.engine.Executioner().ExecuteCommand[reservedDrainCmd](reservedRequest{})
}

func (w *reservedWorld) read(t *testing.T) {
	t.Helper()
	w.engine.Executioner().ExecuteCommand[reservedRecordCmd](reservedRequest{})
}

// TestADeferringSpawnHandleHasOneExportedMethod is the whole of the handle's
// surface. There is no Len, no Pending, no Clear and no Drain: nothing reports
// what is queued, capacity belongs to ShrinkCmd, and Drain stays on
// WriteableEntities so that a System which drains names the wide lock in its
// signature.
//
// New takes the Component set and returns an Entity, exactly as Spawn's does.
// That the Entity is not alive is what the type in the signature says and the
// doc comment leads with; there is nothing at the call site to say it.
func TestADeferringSpawnHandleHasOneExportedMethod(t *testing.T) {
	handle := reflect.TypeFor[*DeferredSpawn[reservedSet]]()
	if handle.NumMethod() != 1 {
		names := make([]string, handle.NumMethod())
		for i := range names {
			names[i] = handle.Method(i).Name
		}
		t.Fatalf("*DeferredSpawn exports %v, want New alone", names)
	}
	method := handle.Method(0)
	if method.Name != "New" {
		t.Fatalf("*DeferredSpawn's one method is %s, want New", method.Name)
	}
	if method.Type.NumIn() != 2 || method.Type.In(1) != reflect.TypeFor[reservedSet]() {
		t.Fatalf("New takes %v, want one reservedSet", method.Type)
	}
	if method.Type.NumOut() != 1 || method.Type.Out(0) != reflect.TypeFor[Entity]() {
		t.Fatalf("New returns %v, want one Entity", method.Type)
	}
}

// TestNewLeadsWithTheNotAliveRule keeps the one sentence that pays for the name.
// New hands back an Entity from both handles and only this one's is reserved, so
// the doc comment is where an author is told — and it is the first thing it
// says, mirroring Spawn[S].New's "the Entity is complete when New returns".
func TestNewLeadsWithTheNotAliveRule(t *testing.T) {
	opening, _, _ := strings.Cut(docOf(t, "deferredspawn.go", "New"), "\n\n")
	if !strings.Contains(opening, "not alive") {
		t.Fatalf("DeferredSpawn.New's doc comment opens with %q; it leads with the not-alive rule", opening)
	}
}

// docOf is the doc comment of a method declared in one of this package's source
// files, read out of the source rather than asked of the built package, because
// a doc comment is not compiled into anything a test can reach.
func docOf(t *testing.T, source, method string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", source, err)
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if ok && fn.Name.Name == method && fn.Doc != nil {
			return fn.Doc.Text()
		}
	}
	t.Fatalf("%s declares no documented %s", source, method)
	return ""
}

type deferringSpawnSystem kernel.Subscription[app.UpdateEvent]

// TestADeferringSpawnerDeclaresReadsAndNothingElse is what the whole design
// buys, read off the engine's own description: a System that creates Entities
// holds read{*Entities} and read{*Store[F]} per Component set field rather than
// the wide write and a write per Store, so it runs beside every Query and every
// accessor and is excluded only against writers of what it spawns.
//
// The write declareSet keeps is redundant for locking and kept for ownership, so
// nothing is lost by making it a read: the kernel's coupling check walks the
// read set through the same closure it walks the write set through.
func TestADeferringSpawnerDeclaresReadsAndNothingElse(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		registrar.Subscribe[deferringSpawnSystem](ToHandler[app.UpdateEvent](registrar,
			func(spawn *DeferredSpawn[reservedSet]) {}))
	})

	want := []reflect.Type{
		entitiesType,
		reflect.TypeFor[*Store[body]](),
		reflect.TypeFor[*Store[collider]](),
		reflect.TypeFor[*Store[homing]](),
	}
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[deferringSpawnSystem]() {
			continue
		}
		reads := append([]reflect.Type(nil), subscription.Reads...)
		slices.SortFunc(reads, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) })
		slices.SortFunc(want, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) })
		if !slices.Equal(reads, want) {
			t.Fatalf("a deferring spawner reads %v, want %v", reads, want)
		}
		if len(subscription.Writes) != 0 || len(subscription.Uses) != 0 {
			t.Fatalf("a deferring spawner writes %v and uses %v; want neither",
				subscription.Writes, subscription.Uses)
		}
		return
	}
	t.Fatalf("the architecture has no deferring spawn System")
}

// TestErrUndeclaredDependencyStillFiresForADeferringSpawn is the ownership rule
// surviving the narrowing. The write per Component set field was kept for
// exactly one reason — no plugin fabricates another plugin's Components — and
// the read keeps it: checkCoupling walks the read set and the write set through
// the same closure, so the refusal fires with its real message, naming the
// Component and its owner.
func TestErrUndeclaredDependencyStillFiresForADeferringSpawn(t *testing.T) {
	compose := func(deps []kernel.PluginName) error {
		var failure error
		kernel.New(nil).
			Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
			WithPlugins(authority{ids: 8}, &componentsPlugin{ids: 8}, &systemsPlugin{deps: deps,
				subscribe: func(registrar *kernel.Registrar) {
					registrar.Subscribe[deferringSpawnSystem](ToHandler[app.UpdateEvent](registrar,
						func(spawn *DeferredSpawn[reservedSet]) {}))
				}})
		return failure
	}

	err := compose([]kernel.PluginName{Name})
	var undeclared kernel.ErrUndeclaredDependency
	if !errors.As(err, &undeclared) {
		t.Fatalf("a deferring spawner over another plugin's Components composed with %v", err)
	}
	if undeclared.Plugin != "systems" || undeclared.Owner != "components" {
		t.Fatalf("the refusal is %+v, want systems locking a Store owned by components", undeclared)
	}
	if !strings.Contains(undeclared.Resource.String(), "Store[") {
		t.Fatalf("the refusal names %v, want the Store of a Component set field", undeclared.Resource)
	}

	if err := compose([]kernel.PluginName{Name, "components"}); err != nil {
		t.Fatalf("a deferring spawner declaring the Components' owner did not compose: %v", err)
	}
}

// TestAReservationIsOneAtomicAddOverTheFreeListThenPastTheEnd is the cursor
// itself, asked of the authority directly: below the free list's length it
// hands out the indices alloc would have taken, newest first, and past it the
// indices come from past len(gens) at the floor. Nothing is written while
// reservations are outstanding — the free list still holds what it held, and
// the index space has not grown — which is what lets the reservation run under
// read{*Entities}.
//
// The settle between the passes is the other half: the free list is cut back to
// where the cursor left it and the cursor is reset, so a drained world's free
// list is ordinary again.
func TestAReservationIsOneAtomicAddOverTheFreeListThenPastTheEnd(t *testing.T) {
	en := newEntities(8)
	made := []Entity{en.alloc(), en.alloc(), en.alloc()}
	en.despawn(made[2])
	en.despawn(made[1])
	freed := slices.Clone(en.free)

	reserved := []Entity{en.reserve(), en.reserve(), en.reserve()}

	if en.reserved.Load() != 3 {
		t.Fatalf("three reservations moved the cursor to %d, want 3: one atomic add each",
			en.reserved.Load())
	}
	wantIndices := []uint32{freed[1], freed[0], 3}
	for i, e := range reserved {
		if e.idx() != wantIndices[i] {
			t.Fatalf("reservation %d took index %d, want %d: the free list newest first, then past len(gens)",
				i, e.idx(), wantIndices[i])
		}
		if en.Alive(e) {
			t.Fatalf("the reserved %v is alive before the drain", e)
		}
	}
	if reserved[2].gen() != en.floor {
		t.Fatalf("a reservation past the end carries generation %d, want the floor %d",
			reserved[2].gen(), en.floor)
	}
	if !slices.Equal(en.free, freed) || len(en.gens) != 3 {
		t.Fatalf("a reservation wrote the authority: free %v and %d generations, want %v and 3",
			en.free, len(en.gens), freed)
	}

	en.enrolDrainSpawn(func() {
		for _, e := range reserved {
			en.spawnReserved(e)
		}
	})
	en.drain()

	for _, e := range reserved {
		if !en.Alive(e) {
			t.Fatalf("the reserved %v is not alive after the drain", e)
		}
	}
	if len(en.free) != 0 {
		t.Fatalf("the settle left %v on the free list, want the reserved indices cut off", en.free)
	}
	if en.reserved.Load() != 0 {
		t.Fatalf("the settle left the cursor at %d, want 0", en.reserved.Load())
	}
	if len(en.gens) != 4 {
		t.Fatalf("the spawn pass left %d generations, want 4: it grows the index space to reach a reservation past the end",
			len(en.gens))
	}
}

// TestAReservedEntityIsNotAliveUntilTheDrain is the price of the naming, stated
// as a rule rather than a remark. Before the drain the Entity New returned does
// not exist by any route there is — Alive is false, no Store holds anything for
// it, no Query yields it, and every immediate handle misses on it exactly as on
// any Entity that is not alive. After the drain it is alive carrying exactly the
// Component set it was queued with, and nothing else.
func TestAReservedEntityIsNotAliveUntilTheDrain(t *testing.T) {
	w := newReservedWorld(t)

	var reserved Entity
	w.queued(t, func(spawn *DeferredSpawn[reservedSet]) {
		reserved = spawn.New(reservedSet{Body: body{X: 1, Y: 2}, Collider: collider{Radius: 3}})
	})

	if w.entities.Alive(reserved) {
		t.Fatalf("the reserved %v is alive before the drain", reserved)
	}
	if w.components.bodies.Len() != 0 || w.components.colliders.Len() != 0 {
		t.Fatalf("a queued Spawn wrote %d bodies and %d colliders at the call, want none",
			w.components.bodies.Len(), w.components.colliders.Len())
	}
	// The queuer sees nothing of its own queue either: a Spawn it queued does not
	// exist for it.
	var visited []Entity
	var retired, removed, reached bool
	w.probed(t, func(it probing) {
		visited = nil
		for e := range it.q.All() {
			visited = append(visited, e)
		}
		retired = it.we.Despawn(reserved)
		it.set.UpdateFor(reserved, body{X: 9})
		removed = it.remove.From(reserved)
		_, reached = it.bodies.Of(reserved)
	})
	if len(visited) != 0 {
		t.Fatalf("a Query visited %v before the drain, want nothing: a reserved Entity does not exist yet", visited)
	}
	if retired {
		t.Fatalf("an immediate Despawn of the reserved %v reported a hit", reserved)
	}
	if removed {
		t.Fatalf("an immediate Remove.From the reserved %v reported a hit", reserved)
	}
	if reached {
		t.Fatalf("an accessor reached the reserved %v before the drain", reserved)
	}
	if w.components.bodies.Len() != 0 {
		t.Fatalf("an immediate Set.UpdateFor gave the reserved %v a body before the drain", reserved)
	}

	w.drained(t)

	if !w.entities.Alive(reserved) {
		t.Fatalf("the reserved %v is not alive after the drain", reserved)
	}
	if value, ok := w.components.bodies.Get(reserved); !ok || value != (body{X: 1, Y: 2}) {
		t.Fatalf("the drained %v has body %v, %v; want {1 2}, true — the set it was queued with",
			reserved, value, ok)
	}
	if value, ok := w.components.colliders.Get(reserved); !ok || value != (collider{Radius: 3}) {
		t.Fatalf("the drained %v has collider %v, %v; want {3}, true", reserved, value, ok)
	}
	if _, ok := w.components.homings.Get(reserved); !ok {
		t.Fatalf("the drained %v has no homing, which its Component set names", reserved)
	}
	if w.components.velocities.Len() != 0 {
		t.Fatalf("the drained %v gained a velocity, which its Component set does not name", reserved)
	}
}

// TestAReservedEntityIsStoredAsAReferenceInTheSameRun is what a Reserved Entity
// is for, and it is why returning nothing was rejected: without an Entity at the
// call, every spawner that links what it spawned would have to be split into two
// Systems, which is the very split deferral exists to avoid.
//
// The Reference resolves to nothing until the drain, as a Reference to any
// Entity that is not alive does, and to the Entity after it.
func TestAReservedEntityIsStoredAsAReferenceInTheSameRun(t *testing.T) {
	w := newReservedWorld(t)

	var target, missile Entity
	w.queued(t, func(spawn *DeferredSpawn[reservedSet]) {
		target = spawn.New(reservedSet{Body: body{X: 7}})
		missile = spawn.New(reservedSet{Homing: homing{Target: target}})
	})
	if target == missile || target == NoEntity {
		t.Fatalf("two reservations handed back %v and %v", target, missile)
	}

	var named Entity
	var reached bool
	w.probed(t, func(it probing) {
		_, reached = it.bodies.Of(target)
	})
	if reached {
		t.Fatalf("a Reference to the reserved %v resolved before the drain", target)
	}

	w.drained(t)

	w.probed(t, func(it probing) {
		held, _ := w.components.homings.Get(missile)
		named = held.Target
		_, reached = it.bodies.Of(named)
	})
	if named != target {
		t.Fatalf("the drained %v names %v, want the %v it was queued with", missile, named, target)
	}
	if !reached {
		t.Fatalf("a Reference to %v did not resolve after the drain", target)
	}
}

// TestTheSpawnPassRunsHandlesInEnrolmentOrderAndBuffersInQueueOrder fixes what
// enrolment order means, because a Store's log and its rows follow it: handles
// in the order they prepared — parameter order within a System, registration
// order between Systems — and each buffer in the order that handle queued.
//
// It is asked by queuing in exactly the wrong order and reading the rows back:
// the second parameter queues first, and a System registered later queues before
// one registered earlier.
func TestTheSpawnPassRunsHandlesInEnrolmentOrderAndBuffersInQueueOrder(t *testing.T) {
	w := newReservedWorld(t)

	var order []Entity
	w.lately(t, func(spawn *DeferredSpawn[reservedSet]) {
		order = append(order, spawn.New(reservedSet{Body: body{X: 4}}))
	})
	w.paired(t, func(first, second *DeferredSpawn[reservedSet]) {
		order = append(order, second.New(reservedSet{Body: body{X: 3}}))
		order = append(order, first.New(reservedSet{Body: body{X: 1}}))
		order = append(order, first.New(reservedSet{Body: body{X: 2}}))
	})

	w.drained(t)

	// The rows were appended in the order the pass applied them, so the Store's
	// own owners are the answer.
	applied := slices.Clone(w.components.bodies.owners)
	want := []Entity{order[2], order[3], order[1], order[0]}
	if !slices.Equal(applied, want) {
		t.Fatalf("the spawn pass applied %v, want %v: handles in enrolment order, each buffer in queue order",
			applied, want)
	}
	for i, e := range applied {
		if value, _ := w.components.bodies.Get(e); value.X != float32(i)+1 {
			t.Fatalf("the %dth Entity the pass applied carries body %v, want X %d", i, value, i+1)
		}
	}
}

// TestAQueuedSpawnAndDespawnOfTheSameEntityAreNotCollapsed is why the drain is
// two passes rather than one walk per handle: every handle's Spawns are applied
// before any handle's Despawns, so a queued Despawn can name a Reserved Entity
// no handle has spawned yet.
//
// The two are not collapsed. The Entity is spawned in the spawn pass and
// despawned in the despawn pass, and both are recorded, because collapsing would
// save a few hundred nanoseconds and lie to every index reading the log.
func TestAQueuedSpawnAndDespawnOfTheSameEntityAreNotCollapsed(t *testing.T) {
	w := newReservedWorld(t)

	var doomed Entity
	w.cycled(t, func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) {
		doomed = spawn.New(reservedSet{Collider: collider{Radius: 5}})
		despawn.Despawn(doomed)
	})

	w.drained(t)
	w.read(t)

	if w.entities.Alive(doomed) {
		t.Fatalf("%v survived a drain that queued its Spawn and its Despawn", doomed)
	}
	if w.components.colliders.Len() != 0 {
		t.Fatalf("a drain that spawned and despawned %v left %d colliders, want none",
			doomed, w.components.colliders.Len())
	}
	expectHeard(t, "what one drain recorded for a Spawn and a Despawn of the same Entity", w.recorded,
		[]heard{{doomed, "spawned+added+changed", 5}, {doomed, "despawned+removed", 5}})
}

// TestAnImmediateAndADeferringSpawnHandleInOneSystemCompose is the half-migrated
// System, and it is legal, silent and checked nowhere: a registration panic
// would be the ECS telling an author their System is pointless rather than
// wrong. The write subsumes the read — GetWrite deletes the type from the read
// set and GetRead skips a type already in the write set — so the two maps are
// disjoint by construction and the order the handles prepare in does not matter.
func TestAnImmediateAndADeferringSpawnHandleInOneSystemCompose(t *testing.T) {
	w := newReservedWorld(t)

	for _, subscription := range w.engine.Describe().Commands {
		if subscription.Type != reflect.TypeFor[reservedBothCmd]() {
			continue
		}
		if slices.Contains(subscription.Reads, entitiesType) {
			t.Fatalf("a System holding both spawn handles reads %v; the write subsumes the read",
				subscription.Reads)
		}
		if !slices.Contains(subscription.Writes, entitiesType) {
			t.Fatalf("a System holding both spawn handles writes %v, want write{*Entities} among them",
				subscription.Writes)
		}
		for _, read := range subscription.Reads {
			if slices.Contains(subscription.Writes, read) {
				t.Fatalf("%v is in both the read set and the write set", read)
			}
		}
	}

	var immediate, deferred Entity
	w.composed(t, func(sp *Spawn[reservedSet], queue *DeferredSpawn[reservedSet]) {
		immediate = sp.New(reservedSet{Body: body{X: 1}})
		deferred = queue.New(reservedSet{Body: body{X: 2}})
	})

	if !w.entities.Alive(immediate) {
		t.Fatalf("an immediate Spawn beside a deferring one did not land at the call")
	}
	if w.entities.Alive(deferred) {
		t.Fatalf("a deferred Spawn beside an immediate one landed at the call")
	}
	w.drained(t)
	if !w.entities.Alive(deferred) {
		t.Fatalf("the deferred Spawn of a System that also spawns immediately did not land at the drain")
	}
	if immediate.idx() == deferred.idx() {
		t.Fatalf("an immediate and a deferred Spawn in one run both took index %d", immediate.idx())
	}
}

type twoDeferringSpawnersSystem kernel.Subscription[app.UpdateEvent]

// TestTwoDeferringSpawnHandlesOfOneSetCompose is the fourth of the four cases
// the spec allows: two independent buffers on the same Component set, drained in
// enrolment order. Nothing is shared between them, and registration is silent.
func TestTwoDeferringSpawnHandlesOfOneSetCompose(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		registrar.Subscribe[twoDeferringSpawnersSystem](ToHandler[app.UpdateEvent](registrar,
			func(first *DeferredSpawn[reservedSet], second *DeferredSpawn[reservedSet]) {}))
	})

	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[twoDeferringSpawnersSystem]() {
			continue
		}
		if len(subscription.Writes) != 0 {
			t.Fatalf("two deferring spawn handles in one System write %v, want nothing", subscription.Writes)
		}
		return
	}
	t.Fatalf("two deferring spawn handles of one Component set did not compose")
}

// TestAQueueOfSpawnsSurvivesASystemInvokedTwiceBeforeADrain is why nothing
// empties a buffer but a drain. resolve runs once per invocation, not once a
// tick, so a handle that reset its buffer there would silently lose the first
// run's queue whenever a System was invoked twice before a drain — which is what
// a System subscribed to an event published twice in a frame is.
func TestAQueueOfSpawnsSurvivesASystemInvokedTwiceBeforeADrain(t *testing.T) {
	w := newReservedWorld(t)

	var made []Entity
	queue := func(spawn *DeferredSpawn[reservedSet]) {
		made = append(made, spawn.New(reservedSet{Body: body{X: float32(len(made)) + 1}}))
	}
	w.queued(t, queue)
	w.queued(t, queue)

	w.drained(t)

	for i, e := range made {
		if !w.entities.Alive(e) {
			t.Fatalf("%v did not survive the drain: the queue of one of the two runs was lost", e)
		}
		if value, _ := w.components.bodies.Get(e); value.X != float32(i)+1 {
			t.Fatalf("%v carries body %v, want X %d", e, value, i+1)
		}
	}
	if len(made) != 2 {
		t.Fatalf("two invocations queued %d Spawns", len(made))
	}
}

// TestTheDrainEmptiesTheSpawnBufferAndKeepsItsCapacity is the steady-state
// claim, and ShrinkCmd is how the capacity comes back. A deferral buffer is
// Scratch — the same area a Query's walk and a writer's row copies are — so
// KeepScratch opts it out and ShrinkResponse.Scratch counts its bytes, with no
// new Keep flag and no new area.
func TestTheDrainEmptiesTheSpawnBufferAndKeepsItsCapacity(t *testing.T) {
	const queued = 32
	var handle *DeferredSpawn[reservedSet]
	_, _, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		// One deferring System and one drain System, and neither holds a Query or
		// a row copy, so every Scratch byte the shrink reports is the buffer's.
		registrar.HandleCommand[reservedQueueCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			func(spawn *DeferredSpawn[reservedSet]) {
				handle = spawn
				for range queued {
					spawn.New(reservedSet{})
				}
			}))
		registrar.HandleCommand[reservedDrainCmd](ToExecute[reservedRequest, reservedResponse](registrar,
			drainSystem))
	})
	executioner := engine.Executioner()

	executioner.ExecuteCommand[reservedQueueCmd](reservedRequest{})
	if len(handle.queued) != queued {
		t.Fatalf("a handle queued %d Spawns and holds %d", queued, len(handle.queued))
	}

	executioner.ExecuteCommand[reservedDrainCmd](reservedRequest{})

	if len(handle.queued) != 0 {
		t.Fatalf("a drained buffer holds %d queued Spawns, want none", len(handle.queued))
	}
	want := uintptr(queued) * unsafe.Sizeof(queuedSpawn[reservedSet]{})
	if uintptr(cap(handle.queued))*unsafe.Sizeof(queuedSpawn[reservedSet]{}) < want {
		t.Fatalf("a drained buffer kept capacity for %d queued Spawns, want at least %d",
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
		t.Fatalf("a shrink reported %d Scratch bytes for a buffer of %d queued Spawns, want at least %d",
			released.Scratch, queued, want)
	}
	if cap(handle.queued) != 0 {
		t.Fatalf("a shrink left %d queued Spawns of capacity in the deferral buffer", cap(handle.queued))
	}
}

type spawnCycleSystem kernel.Subscription[app.UpdateEvent]

// deferringSpawnCycle queues a Spawn, queues a Despawn of one Entity and drains,
// so the population is what it was and both buffers are appended to and emptied
// every frame. immediateSpawnCycle is the same System with the Spawn made at the
// call, and is what the deferring arm is read against.
//
// The two are written out rather than branched on a captured flag, because a
// System func that closes over one costs three objects a frame through
// reflect.Value.Call — a measurement artefact that would swamp what is measured
// here.
func deferringSpawnCycle(spawn *DeferredSpawn[spawnSet], we *WriteableEntities, despawn *DeferredDespawn, q *Query[moveQuery]) {
	for e := range q.All() {
		despawn.Despawn(e)
		break
	}
	spawn.New(spawnSet{Velocity: velocity{X: 1}})
	we.Drain()
}

func immediateSpawnCycle(spawn *Spawn[spawnSet], we *WriteableEntities, despawn *DeferredDespawn, q *Query[moveQuery]) {
	for e := range q.All() {
		despawn.Despawn(e)
		break
	}
	spawn.New(spawnSet{Velocity: velocity{X: 1}})
	we.Drain()
}

// TestADeferredSpawnSitsOnTheEnginesAllocationLine measures a steady state
// rather than an idle world: every frame queues a Spawn, queues a Despawn and
// drains both, so the population is what it was and both buffers are appended to
// and emptied every frame. A buffer that grew, a drain that rebuilt one, or a
// reservation that boxed its Component set would allocate whatever the
// population, so identical counts at 1k and 10k are the claim and the
// 6-per-frame line is the engine's own.
//
// The whole cycle is one System, which is the composed case the spec allows and
// calls honest, and it is measured this way because the engine charges a frame
// for each subscription node it dispatches, whatever the node does.
func TestADeferredSpawnSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const frames = 10_000
	cycle := func(system any) func(*kernel.Registrar) {
		return func(registrar *kernel.Registrar) {
			registrar.Subscribe[spawnCycleSystem](ToHandler[app.UpdateEvent](registrar, system))
		}
	}
	measure := func(n int, subscribe func(*kernel.Registrar)) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		populate(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, both deferral buffers included,
		// so what is measured is steady state and not the first tick.
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

	at1k, at10k := measure(1_000, cycle(deferringSpawnCycle)), measure(10_000, cycle(deferringSpawnCycle))
	immediate := measure(1_000, cycle(immediateSpawnCycle))
	t.Logf("objects a frame, one node spawning, despawning and draining: deferred at 1k %.3f, at 10k %.3f; immediate at 1k %.3f",
		at1k, at10k, immediate)

	if at10k > at1k+0.05 {
		t.Fatalf("a deferring frame allocates with the entity count: %.3f a frame at 1k, %.3f at 10k", at1k, at10k)
	}
	if at10k > 6.5 {
		t.Fatalf("a deferring frame costs %.3f objects, above the engine's 6-per-frame line", at10k)
	}
	if at1k > immediate+0.05 {
		t.Fatalf("deferring a Spawn costs %.3f objects a frame against the immediate %.3f", at1k, immediate)
	}
}

// TestTheRefusalNamesTheDeferringSpawnHandle keeps the hand-maintained list of
// legal System parameters honest: the sentence a bad signature gets is where an
// author finds out the handle exists.
func TestTheRefusalNamesTheDeferringSpawnHandle(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System taking an unknown parameter was accepted at registration")
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, "*ecs.DeferredSpawn") {
			t.Fatalf("the refusal %q does not name *ecs.DeferredSpawn", message)
		}
	}()
	ToHandler[app.UpdateEvent](nil, func(s *Store[body]) {})
}
