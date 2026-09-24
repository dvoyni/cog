package internal

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of deferred.md § Hooks: a drained change is recorded at
// the drain, with the same records as an immediate one, and a deferring handle
// records nothing at the call and declares nothing for Hooks.
//
// Each System here is invoked as a command, so a test decides exactly which
// System runs when — the queuer's run, then the drain's, then the reader's —
// which is what makes "before the drain" and "after the drain" statements about
// an order rather than about a schedule. The one test about a real schedule
// subscribes to app.UpdateEvent instead, and says so.

type (
	drainedRequest  struct{}
	drainedResponse struct{}
)

type (
	drainedCycleCmd drainedCommand
	drainedOrderCmd drainedCommand
	drainedWriteCmd drainedCommand
	drainedDrainCmd drainedCommand
	drainedReadCmd  drainedCommand
)

type drainedCommand = kernel.Command[drainedRequest, drainedResponse]

// drainedOrdering is four deferring handles in one System, in the order they
// prepare: two spawn handles and two despawn handles. Parameter order within a
// System is enrolment order, so it is what the drain's two passes walk in.
type drainedOrdering struct {
	firstIn, secondIn   *DeferredSpawn[reservedSet]
	firstOut, secondOut *DeferredDespawn
}

// drainedWorld is a world of deferring Systems, a drain System and one Hooks
// reader of collider, each a command. The reader is HookSpawnedDespawned, which
// is what a drained structural change records and nothing besides.
type drainedWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	cycle      func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn)
	order      func(it drainedOrdering)
	write      func(set *Set[collider])
	// heard is what the reader heard since the last read, which is how "records
	// nothing" and "records both" are asked.
	heard []heard
	// reported is the last error the handler saw, which is where a reader that
	// falls behind the pace check arrives now that a dispatch does not hand a
	// panic back.
	reported error
}

func newDrainedWorld(t *testing.T) *drainedWorld {
	t.Helper()
	w := &drainedWorld{}
	w.entities, w.components, w.engine = newWorldHandling(t, 64, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[drainedCycleCmd](ToExecute[drainedRequest, drainedResponse](registrar,
			func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) { w.cycle(spawn, despawn) }))
		registrar.HandleCommand[drainedOrderCmd](ToExecute[drainedRequest, drainedResponse](registrar,
			func(firstIn, secondIn *DeferredSpawn[reservedSet], firstOut, secondOut *DeferredDespawn) {
				w.order(drainedOrdering{firstIn, secondIn, firstOut, secondOut})
			}))
		registrar.HandleCommand[drainedWriteCmd](ToExecute[drainedRequest, drainedResponse](registrar,
			func(set *Set[collider]) { w.write(set) }))
		registrar.HandleCommand[drainedDrainCmd](ToExecute[drainedRequest, drainedResponse](registrar,
			typesDrainSystem))
		registrar.HandleCommand[drainedReadCmd](ToExecute[drainedRequest, drainedResponse](registrar,
			func(h *Hooks[collider, HookSpawnedDespawned]) {
				w.heard = w.heard[:0]
				listen(h, &w.heard, radius)
			}))
	}, nil, func(err error) error {
		// A reader past the pace limit panics, and the kernel reports that panic
		// rather than handing it back to the dispatch. The pace test is about the
		// panic, so the world keeps it instead of failing on it.
		w.reported = err
		return nil
	})
	return w
}

// alive gives one Entity a collider and hands it back, so the reader records its
// Despawn with the radius given here.
func (w *drainedWorld) alive(radius float32) Entity {
	e := w.entities.alloc()
	w.components.colliders.Set(e, collider{Radius: radius})
	return e
}

func (w *drainedWorld) cycled(t *testing.T, cycle func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn)) {
	t.Helper()
	w.cycle = cycle
	w.engine.Executioner().ExecuteCommand[drainedCycleCmd](drainedRequest{})
}

func (w *drainedWorld) ordered(t *testing.T, order func(it drainedOrdering)) {
	t.Helper()
	w.order = order
	w.engine.Executioner().ExecuteCommand[drainedOrderCmd](drainedRequest{})
}

func (w *drainedWorld) wrote(t *testing.T, write func(set *Set[collider])) {
	t.Helper()
	w.write = write
	w.engine.Executioner().ExecuteCommand[drainedWriteCmd](drainedRequest{})
}

func (w *drainedWorld) drained(t *testing.T) {
	t.Helper()
	w.engine.Executioner().ExecuteCommand[drainedDrainCmd](drainedRequest{})
}

// read runs the reader and fails the test on anything the handler reported,
// which is every test here but the pace one.
func (w *drainedWorld) read(t *testing.T) {
	t.Helper()
	if err := w.readError(); err != nil {
		t.Fatalf("the reader's run was refused: %v", err)
	}
}

// readError runs the reader and reports what running it reported, clearing it so
// that the next run starts from nothing.
func (w *drainedWorld) readError() error {
	w.reported = nil
	w.engine.Executioner().ExecuteCommand[drainedReadCmd](drainedRequest{})
	return w.reported
}

// TestAHooksReaderSeesADrainedChangeOnlyAfterTheDrain is deferred.md § Hooks'
// first rule made visible from the reader's side: a deferring handle records
// nothing at the call, and the drained spawn and the drained despawn arrive
// together in the reader's first run after the drain, as the same records an
// immediate Spawn and an immediate Despawn would have written.
func TestAHooksReaderSeesADrainedChangeOnlyAfterTheDrain(t *testing.T) {
	w := newDrainedWorld(t)
	doomed := w.alive(3)

	var born Entity
	w.cycled(t, func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) {
		born = spawn.New(reservedSet{Collider: collider{Radius: 7}})
		despawn.Despawn(doomed)
	})

	w.read(t)
	expectHeard(t, "what a reader hears of a queue nothing has drained", w.heard, nil)

	w.drained(t)
	w.read(t)
	expectHeard(t, "what a reader hears in its first run after the drain", w.heard, []heard{
		{born, "spawned+added+changed", 7},
		{doomed, "despawned+removed", 3},
	})

	w.read(t)
	expectHeard(t, "what a reader hears in the run after that", w.heard, nil)
}

// TestADrainedDespawnCarriesTheValueAtTheDrain is deferred.md § Hooks' rule that
// a drained despawn captures each Component's value at the drain, not at the
// call: the capture is the drain's, made under its write, so a write landing
// between the call and the drain is in the record.
//
// It is the point of the rule that the reader cannot tell this despawn from an
// immediate one, which carries the value at the Despawn for the same reason.
func TestADrainedDespawnCarriesTheValueAtTheDrain(t *testing.T) {
	w := newDrainedWorld(t)
	doomed := w.alive(1)

	w.cycled(t, func(_ *DeferredSpawn[reservedSet], despawn *DeferredDespawn) { despawn.Despawn(doomed) })
	w.wrote(t, func(set *Set[collider]) { set.UpdateFor(doomed, collider{Radius: 9}) })
	w.drained(t)
	w.read(t)

	expectHeard(t, "what a drained Despawn recorded after a write between the call and the drain",
		w.heard, []heard{{doomed, "despawned+removed", 9}})
}

// TestADrainedStoresLogFollowsTheDrainOrder is deferred.md § Hooks' order rule
// read off the log rather than off the Store's rows: handles in enrolment order,
// each buffer in queue order, spawn pass before despawn pass, nothing iterating
// a map.
//
// It is asked by queuing in exactly the wrong order — the second spawn handle
// queues first, the second despawn handle queues first, and the despawns are
// queued after the spawns in a System that applies them the other way round.
func TestADrainedStoresLogFollowsTheDrainOrder(t *testing.T) {
	w := newDrainedWorld(t)
	first, second := w.alive(10), w.alive(20)

	var made []Entity
	w.ordered(t, func(it drainedOrdering) {
		made = append(made, it.secondIn.New(reservedSet{Collider: collider{Radius: 3}}))
		made = append(made, it.firstIn.New(reservedSet{Collider: collider{Radius: 1}}))
		made = append(made, it.firstIn.New(reservedSet{Collider: collider{Radius: 2}}))
		it.secondOut.Despawn(second)
		it.firstOut.Despawn(first)
	})

	w.drained(t)
	w.read(t)

	expectHeard(t, "the order one drain appended to a Store's log in", w.heard, []heard{
		{made[1], "spawned+added+changed", 1},
		{made[2], "spawned+added+changed", 2},
		{made[0], "spawned+added+changed", 3},
		{first, "despawned+removed", 10},
		{second, "despawned+removed", 20},
	})
}

// paceRounds queues a Spawn and a Despawn of the same reserved Entity and drains
// them, n times. The two are not collapsed, so every drain here appends in both
// of its passes — which is the drain shape the count is not allowed to measure.
func (w *drainedWorld) paceRounds(t *testing.T, n int) {
	t.Helper()
	for range n {
		w.cycled(t, func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) {
			despawn.Despawn(spawn.New(reservedSet{Collider: collider{Radius: 1}}))
		})
		w.drained(t)
	}
}

// TestOneDrainIsOneWriterRunPerStore is deferred.md § One drain is one writer
// run per Store, against hooks.md's pace check: a reader panics past sixteen
// counted runs, and a drain that appends in both of its passes is one of them.
//
// Sixteen drains that each append twice must not trip the check, and the
// seventeenth must — counted as 17, not as 34. That the number in the panic is
// the number of drains is the whole claim: the count measures how often the
// world was drained, never the shape of the drainer.
func TestOneDrainIsOneWriterRunPerStore(t *testing.T) {
	w := newDrainedWorld(t)

	w.paceRounds(t, 16)
	if err := w.readError(); err != nil {
		t.Fatalf("a reader 16 drains behind was refused, so a two-pass drain is counted more than once: %v", err)
	}

	w.paceRounds(t, 17)
	err := w.readError()
	if !validate {
		if err != nil {
			t.Fatalf("a release build refused a reader 17 drains behind: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("a reader 17 drains behind was not refused, so a drain that appends is not counted at all")
	}
	for _, want := range []string{"Store[ecs.collider]", "17"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the panic does not name %q, so a drain is not counted exactly once: %v", want, err)
		}
	}
}

// TestNeitherDeferringHandleOffersASpawnGate is deferred.md § Hooks' second
// half. systemCall.lock collects a spawn gate from any parameter offering one,
// and enrolPace then marks that System "can append to every Store's log" for
// Validation mode's pace count. A queuing System appends to nothing: the writing
// and the records happen at the drain, which holds write{*Entities} and is the
// Structural change.
//
// The immediate handle is asserted to offer one, so this stays a test about what
// the deferring handles decline rather than one that would pass if the interface
// were deleted.
func TestNeitherDeferringHandleOffersASpawnGate(t *testing.T) {
	if _, ok := systemParam(&Spawn[reservedSet]{}).(spawnGated); !ok {
		t.Fatal("the immediate Spawn handle no longer offers a spawn gate, so this test no longer says anything")
	}
	for _, handle := range []struct {
		name  string
		param systemParam
	}{
		{"*DeferredSpawn[reservedSet]", &DeferredSpawn[reservedSet]{}},
		{"*DeferredDespawn", &DeferredDespawn{}},
	} {
		if _, ok := handle.param.(spawnGated); ok {
			t.Errorf("%s offers a spawn gate: the gate and the pace count belong to the drain, and carrying it here would charge the queuer for a drain it never makes",
				handle.name)
		}
	}
}

// TestASystemHoldingOnlyDeferringHandlesIsPacedOnNoStore is the consequence of
// the above, read off enrolPace itself: a System whose only handles defer names
// no Store to Validation mode's pace count and is not marked as appending to
// every one. A System holding the immediate handle is, which is what the
// deferring one is read against.
func TestASystemHoldingOnlyDeferringHandlesIsPacedOnNoStore(t *testing.T) {
	var deferring systemCall[app.UpdateEvent]
	for _, param := range []systemParam{&DeferredSpawn[reservedSet]{}, &DeferredDespawn{}} {
		deferring.enrolPace(param)
	}
	if deferring.pace.every {
		t.Error("a System holding only deferring handles is marked as appending to every Store's log")
	}
	if len(deferring.pace.stores) != 0 {
		t.Errorf("a System holding only deferring handles names %d Stores to the pace count, want none",
			len(deferring.pace.stores))
	}

	var immediate systemCall[app.UpdateEvent]
	immediate.enrolPace(&Spawn[reservedSet]{})
	if !immediate.pace.every {
		t.Error("a System holding the immediate Spawn handle is not marked as appending to every Store's log")
	}
}

type (
	drainedQueueSystem kernel.Subscription[app.UpdateEvent]
	drainedDrainSystem kernel.Subscription[app.UpdateEvent]
	drainedReadSystem  kernel.Subscription[app.UpdateEvent]
)

// TestAHooksReaderOrderedAfterAnAppDrainSeesTheChangeInThisPublication is
// deferred.md § Hooks' visibility rule, which follows hooks.md's general one
// rather than being special-cased: the System that made the change is the drain
// System, so a reader ordered after it sees the change on that same
// publication — this supersedes the constraint's earlier "a reader on the same
// event sees it in its next run", which assumed a Last() drain only.
func TestAHooksReaderOrderedAfterAnAppDrainSeesTheChangeInThisPublication(t *testing.T) {
	var doomed, born Entity
	var queued bool
	var recorded []heard
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.Subscribe[drainedQueueSystem](ToHandler[app.UpdateEvent](registrar,
			func(spawn *DeferredSpawn[reservedSet], despawn *DeferredDespawn) {
				if queued {
					return
				}
				queued = true
				born = spawn.New(reservedSet{Collider: collider{Radius: 7}})
				despawn.Despawn(doomed)
			}))
		registrar.Subscribe[drainedDrainSystem](ToHandler[app.UpdateEvent](registrar,
			typesDrainSystem)).After[drainedQueueSystem]()
		registrar.Subscribe[drainedReadSystem](ToHandler[app.UpdateEvent](registrar,
			func(h *Hooks[collider, HookSpawnedDespawned]) {
				recorded = recorded[:0]
				listen(h, &recorded, radius)
			})).After[drainedDrainSystem]()
	})

	doomed = entities.alloc()
	components.colliders.Set(doomed, collider{Radius: 3})

	frame(t, engine, 1)

	expectHeard(t, "what a reader ordered after an app drain System heard in that publication", recorded,
		[]heard{{born, "spawned+added+changed", 7}, {doomed, "despawned+removed", 3}})
	if slices.Contains([]Entity{doomed}, born) {
		t.Fatalf("the drained Spawn reused the drained Despawn's handle: %v", born)
	}
}
