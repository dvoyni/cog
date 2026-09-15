package types

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of hooks.md, on Systems in a real kernel.Engine. Each
// System here is invoked as a command, so a test decides exactly which System
// runs when: a writer's run, then a reader's, then another writer's.

type (
	hookActCmd     kernel.Command[hookRequest, hookResponse]
	hookReadCmd    kernel.Command[hookRequest, hookResponse]
	hookRestackCmd kernel.Command[hookRequest, hookResponse]
	hookWalkCmd    kernel.Command[hookRequest, hookResponse]
)

// armedSet is a Component set carrying collider; spawnSet carries body and
// velocity and not collider.
type armedSet struct {
	Body     body
	Collider collider
}

// restacking is what a structural writer holds: a Spawn carrying collider, one
// that does not, the despawning handle, and Set for a write no act records.
type restacking struct {
	armed   *Spawn[armedSet]
	unarmed *Spawn[spawnSet]
	we      *WriteableEntities
	set     *Set[collider]
}

type (
	hookRequest  struct{}
	hookResponse struct{}
)

// heard is one delivered record, rendered: the Entity, every kind the record
// reports, and the collider's radius it carries.
type heard struct {
	e     Entity
	kinds string
	value float32
}

func (h heard) String() string { return fmt.Sprintf("%v %s %v", h.e, h.kinds, h.value) }

// possible is, for each kind set, the IsX a record it delivers may be asked:
// hooks.md § What IsX reports. Asking the others is a Validation panic.
func possible[K KindSet]() (spawned, despawned, added, removed, changed bool) {
	var k K
	switch any(k).(type) {
	case HookSpawned, HookAdded, HookAddedChanged:
		return true, false, true, false, true
	case HookDespawned, HookRemoved:
		return false, true, false, true, false
	default:
		return true, true, true, true, true
	}
}

// listen appends every record h delivers to into, in order.
func listen[T any, K KindSet](h *Hooks[T, K], into *[]heard, value func(T) float32) {
	spawned, despawned, added, removed, changed := possible[K]()
	for e, hook := range h.All() {
		var kinds []string
		for _, kind := range []struct {
			asked bool
			is    func() bool
			name  string
		}{
			{spawned, hook.IsSpawned, "spawned"},
			{despawned, hook.IsDespawned, "despawned"},
			{added, hook.IsAdded, "added"},
			{removed, hook.IsRemoved, "removed"},
			{changed, hook.IsChanged, "changed"},
		} {
			if kind.asked && kind.is() {
				kinds = append(kinds, kind.name)
			}
		}
		*into = append(*into, heard{e, strings.Join(kinds, "+"), value(hook.Value)})
	}
}

func radius(c collider) float32 { return c.Radius }

func expectHeard(t *testing.T, what string, got, want []heard) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s:\n got %v\nwant %v", what, got, want)
	}
}

// kindSetReaders is one System reading collider's Hooks under all eight kind
// sets at once. Eight Hooks parameters in one System are eight readers, each
// with its own place in the log.
type kindSetReaders struct {
	spawned, despawned, spawnedDespawned, added, removed, addedRemoved, addedChanged, all []heard
}

func (r *kindSetReaders) system(
	spawned *Hooks[collider, HookSpawned],
	despawned *Hooks[collider, HookDespawned],
	spawnedDespawned *Hooks[collider, HookSpawnedDespawned],
	added *Hooks[collider, HookAdded],
	removed *Hooks[collider, HookRemoved],
	addedRemoved *Hooks[collider, HookAddedRemoved],
	addedChanged *Hooks[collider, HookAddedChanged],
	all *Hooks[collider, HookAll],
) {
	*r = kindSetReaders{}
	listen(spawned, &r.spawned, radius)
	listen(despawned, &r.despawned, radius)
	listen(spawnedDespawned, &r.spawnedDespawned, radius)
	listen(added, &r.added, radius)
	listen(removed, &r.removed, radius)
	listen(addedRemoved, &r.addedRemoved, radius)
	listen(addedChanged, &r.addedChanged, radius)
	listen(all, &r.all, radius)
}

// hookWorld is a world with a scripted writer of collider and a reader System,
// each a command.
type hookWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	act        func(set *Set[collider], remove *Remove[collider])
	restack    func(r restacking)
	walk       func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider])
}

func newHookWorld(t *testing.T, reader any, also ...func(*kernel.Registrar)) *hookWorld {
	t.Helper()
	w := &hookWorld{}
	w.entities, w.components, w.engine = newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookActCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(set *Set[collider], remove *Remove[collider]) { w.act(set, remove) }))
		registrar.HandleCommand[hookRestackCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(armed *Spawn[armedSet], unarmed *Spawn[spawnSet], we *WriteableEntities, set *Set[collider]) {
				w.restack(restacking{armed, unarmed, we, set})
			}))
		registrar.HandleCommand[hookWalkCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider]) { w.walk(q, set, remove) }))
		registrar.HandleCommand[hookReadCmd](ToExecute[hookRequest, hookResponse](registrar, reader))
		for _, subscribe := range also {
			subscribe(registrar)
		}
	})
	return w
}

func (w *hookWorld) write(t *testing.T, act func(set *Set[collider], remove *Remove[collider])) {
	t.Helper()
	w.act = act
	if _, err := w.engine.Executioner().ExecuteCommand[hookActCmd](hookRequest{}); err != nil {
		t.Fatalf("running the writer: %v", err)
	}
}

// walked runs a System holding every write route to collider at once: a
// *collider Query field, Set and Remove.
func (w *hookWorld) walked(t *testing.T, walk func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider])) {
	t.Helper()
	w.walk = walk
	if _, err := w.engine.Executioner().ExecuteCommand[hookWalkCmd](hookRequest{}); err != nil {
		t.Fatalf("running the walking writer: %v", err)
	}
}

// structural runs a System that spawns and despawns.
func (w *hookWorld) structural(t *testing.T, restack func(r restacking)) {
	t.Helper()
	w.restack = restack
	if _, err := w.engine.Executioner().ExecuteCommand[hookRestackCmd](hookRequest{}); err != nil {
		t.Fatalf("running the structural writer: %v", err)
	}
}

func (w *hookWorld) read(t *testing.T) {
	t.Helper()
	if _, err := w.engine.Executioner().ExecuteCommand[hookReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}
}

// TestEachKindSetDeliversWhatItsTableSays is hooks.md § The eight kind sets for
// the two acts an accessor makes: an UpdateFor that adds, and a Remove.From. A
// replacing UpdateFor is not an addition, and an act that does nothing records
// nothing, under every kind set.
func TestEachKindSetDeliversWhatItsTableSays(t *testing.T) {
	readers := &kindSetReaders{}
	w := newHookWorld(t, readers.system)
	a, b, gone := w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(b, collider{Radius: 7})
	w.entities.despawn(gone)

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.UpdateFor(a, collider{Radius: 1}) // adds
		set.UpdateFor(a, collider{Radius: 2}) // replaces: not an addition
		remove.From(a)                        // removes, carrying 2
		set.UpdateFor(b, collider{Radius: 7}) // replaces with the same bytes
		remove.From(gone)                     // does nothing
		set.UpdateFor(gone, collider{})       // does nothing
	})
	w.read(t)

	addition := heard{a, "added+changed", 2}
	removal := heard{a, "removed", 2}
	expectHeard(t, "HookSpawned", readers.spawned, nil)
	expectHeard(t, "HookDespawned", readers.despawned, nil)
	expectHeard(t, "HookSpawnedDespawned", readers.spawnedDespawned, nil)
	expectHeard(t, "HookAdded", readers.added, []heard{addition})
	expectHeard(t, "HookRemoved", readers.removed, []heard{removal})
	expectHeard(t, "HookAddedRemoved", readers.addedRemoved, []heard{addition, removal})
	expectHeard(t, "HookAddedChanged", readers.addedChanged, []heard{addition})
	expectHeard(t, "HookAll", readers.all, []heard{addition, removal})

	w.read(t)
	expectHeard(t, "HookAll, a run with no act since the last", readers.all, nil)
}

// TestEachKindSetDeliversWhatItsTableSaysForSpawnsAndDespawns is hooks.md § The
// eight kind sets for the acts Entities make: a Spawn carrying collider, a Spawn
// not carrying it, a Despawn of an Entity holding it, one of an Entity without
// it, and one of a dead Entity. An UpdateFor addition and a Remove.From ride
// beside them, so HookSpawned and HookDespawned are seen to pass over both.
func TestEachKindSetDeliversWhatItsTableSaysForSpawnsAndDespawns(t *testing.T) {
	readers := &kindSetReaders{}
	w := newHookWorld(t, readers.system)
	held, added, gone := w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(held, collider{Radius: 5})
	w.entities.despawn(gone)

	var armed, unarmed Entity
	var deadReport bool
	w.structural(t, func(r restacking) {
		armed = r.armed.New(armedSet{Collider: collider{Radius: 1}}) // spawned+added+changed
		unarmed = r.unarmed.New(spawnSet{Body: body{X: 1}})          // nothing for collider
		r.set.UpdateFor(added, collider{Radius: 2})                  // added+changed, no Spawn
		r.we.Despawn(held)                                           // despawned+removed, carrying 5
		r.we.Despawn(unarmed)                                        // holds no collider: nothing
		deadReport = r.we.Despawn(gone)                              // dead: nothing
	})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(added) // removed, no Despawn
	})
	w.read(t)

	if deadReport {
		t.Fatal("despawning a dead Entity reported true")
	}
	spawn := heard{armed, "spawned+added+changed", 1}
	addition := heard{added, "added+changed", 2}
	despawn := heard{held, "despawned+removed", 5}
	removal := heard{added, "removed", 2}
	expectHeard(t, "HookSpawned", readers.spawned, []heard{spawn})
	expectHeard(t, "HookDespawned", readers.despawned, []heard{despawn})
	expectHeard(t, "HookSpawnedDespawned", readers.spawnedDespawned, []heard{spawn, despawn})
	expectHeard(t, "HookAdded", readers.added, []heard{spawn, addition})
	expectHeard(t, "HookRemoved", readers.removed, []heard{despawn, removal})
	expectHeard(t, "HookAddedRemoved", readers.addedRemoved, []heard{spawn, addition, despawn, removal})
	expectHeard(t, "HookAddedChanged", readers.addedChanged, []heard{spawn, addition})
	expectHeard(t, "HookAll", readers.all, []heard{spawn, addition, despawn, removal})
}

// TestADespawnCarriesTheValueAtTheDespawn is hooks.md § Values, and a window,
// for Entities: a Despawn's record carries collider as it stood at the Despawn,
// after writes no act records, and a spawn despawned in the same window carries
// the Despawn's value, not the zero value and not that of the Entity that reuses
// its index. The spawns are read under HookSpawned, which is not given the
// removals that fix their values.
func TestADespawnCarriesTheValueAtTheDespawn(t *testing.T) {
	var spawned, despawned []heard
	w := newHookWorld(t, func(s *Hooks[collider, HookSpawned], d *Hooks[collider, HookDespawned]) {
		spawned, despawned = spawned[:0], despawned[:0]
		listen(s, &spawned, radius)
		listen(d, &despawned, radius)
	})

	var early, brief, after Entity
	w.structural(t, func(r restacking) {
		early = r.armed.New(armedSet{Collider: collider{Radius: 1}})
	})
	w.read(t)
	expectHeard(t, "HookSpawned, the first window", spawned, []heard{{early, "spawned+added+changed", 1}})
	expectHeard(t, "HookDespawned, the first window", despawned, nil)

	w.structural(t, func(r restacking) {
		ref, _ := r.set.Ref(early)
		ref.Radius = 2
		r.we.Despawn(early)
		brief = r.armed.New(armedSet{Collider: collider{Radius: 3}})
		ref, _ = r.set.Ref(brief)
		ref.Radius = 4
		r.we.Despawn(brief)
		after = r.armed.New(armedSet{Collider: collider{Radius: 9}})
	})
	if brief.idx() != early.idx() || after.idx() != brief.idx() {
		t.Fatalf("the harness did not recycle the index: %v, %v, %v", early, brief, after)
	}
	w.read(t)
	expectHeard(t, "HookSpawned", spawned, []heard{
		{brief, "spawned+added+changed", 4},
		{after, "spawned+added+changed", 9},
	})
	expectHeard(t, "HookDespawned", despawned, []heard{
		{early, "despawned+removed", 2},
		{brief, "despawned+removed", 4},
	})
}

type hookSpawnEveryCmd kernel.Command[hookRequest, hookResponse]

// everySet carries four Components: two whose Stores are watched for Spawns,
// one watched only for Despawns, and one nobody reads. collider, which it does
// not carry, is watched for everything.
type everySet struct {
	Body     body
	Velocity velocity
	Homing   homing
	Solid    solid
}

// TestASpawnRecordsOnEveryWatchedStoreItCarriesAndNoOther is hooks.md § Which
// acts are recorded for a Spawn: one record on each Store it carries that is
// watched for Spawns, additions or changes, and nothing on a carried Store
// watched only for Despawns, on a watched Store it does not carry, or on a Store
// nobody reads.
func TestASpawnRecordsOnEveryWatchedStoreItCarriesAndNoOther(t *testing.T) {
	var bodies, homings, colliders int
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookSpawnEveryCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(sp *Spawn[everySet]) { sp.New(everySet{Body: body{X: 1}}) }))
		registrar.HandleCommand[hookReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(b *Hooks[body, HookSpawned], h *Hooks[homing, HookAddedChanged],
				v *Hooks[velocity, HookDespawned], c *Hooks[collider, HookAll],
			) {
				for range b.All() {
					bodies++
				}
				for range h.All() {
					homings++
				}
				for range c.All() {
					colliders++
				}
			}))
	})
	if _, err := engine.Executioner().ExecuteCommand[hookSpawnEveryCmd](hookRequest{}); err != nil {
		t.Fatalf("running the spawner: %v", err)
	}

	spawnedKinds := kindSpawned | kindAdded | kindChanged
	for _, log := range []struct {
		name    string
		records []hookRecord
		want    int
	}{
		{"body, watched for Spawns and carried", components.bodies.hooks.records, 1},
		{"homing, watched for additions and changes and carried", components.homings.hooks.records, 1},
		{"velocity, watched only for Despawns and carried", components.velocities.hooks.records, 0},
		{"collider, watched for everything and not carried", components.colliders.hooks.records, 0},
	} {
		if len(log.records) != log.want {
			t.Fatalf("%s: the log holds %d records, want %d", log.name, len(log.records), log.want)
		}
		if log.want == 1 && (log.records[0].kinds != spawnedKinds || !entities.Alive(log.records[0].e)) {
			t.Fatalf("%s: the record is %+v, want spawned+added+changed of the live Entity", log.name, log.records[0])
		}
	}
	if components.solids.hooks != nil {
		t.Fatal("solid, which nobody reads, has a log")
	}

	if _, err := engine.Executioner().ExecuteCommand[hookReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}
	if bodies != 1 || homings != 1 || colliders != 0 {
		t.Fatalf("the readers were given %d bodies, %d homings and %d colliders, want 1, 1 and 0", bodies, homings, colliders)
	}
}

// TestAnAdditionCarriesTheValueAtItsNextRemoval is hooks.md § Values, and a
// window, on a Store only HookAdded watches: nobody reads removals, and the
// removal is still recorded, because it fixes the addition's value. An addition
// not removed since carries the value at the reader's run start, whoever wrote
// it.
func TestAnAdditionCarriesTheValueAtItsNextRemoval(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAdded]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	removed, kept := w.entities.alloc(), w.entities.alloc()

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.UpdateFor(removed, collider{Radius: 1})
		set.UpdateFor(kept, collider{Radius: 10})
	})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(removed)
		ref.Radius = 2
		remove.From(removed)
		set.UpdateFor(removed, collider{Radius: 3})
	})
	w.components.colliders.Set(kept, collider{Radius: 11}) // a write no act recorded
	w.read(t)

	expectHeard(t, "HookAdded", got, []heard{
		{removed, "added+changed", 2},
		{kept, "added+changed", 11},
		{removed, "added+changed", 3},
	})
}

// TestARemovalThenAReAdditionIsTwoRecords is "nothing is netted": the Entity
// held collider before the reader's copy began, lost it and gained it again.
func TestARemovalThenAReAdditionIsTwoRecords(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedRemoved]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	e := w.entities.alloc()
	w.components.colliders.Set(e, collider{Radius: 1})

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(e)
		set.UpdateFor(e, collider{Radius: 2})
	})
	w.read(t)

	expectHeard(t, "HookAddedRemoved", got, []heard{
		{e, "removed", 1},
		{e, "added+changed", 2},
	})
}

// TestARecycledIndexIsNeverFoldedWithTheEntityItReplaced carries the full
// Entity in every record. The first Entity's addition is still open when its
// index is reused, and the removal of the Entity that reused it must not fix
// the first one's value. A Despawn of an Entity holding collider records a
// removal that would close the addition, so the first Entity's collider is taken
// straight out of the Store first, which no act records. The second Entity's
// collider is written straight into the Store the same way, so its removal is
// the only record it has.
func TestARecycledIndexIsNeverFoldedWithTheEntityItReplaced(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedRemoved]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	first := w.entities.alloc()

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.UpdateFor(first, collider{Radius: 1})
	})
	w.components.colliders.Remove(first)
	w.entities.despawn(first)
	second := w.entities.alloc()
	if second.idx() != first.idx() || second == first {
		t.Fatalf("the harness did not recycle the index: %v then %v", first, second)
	}
	w.components.colliders.Set(second, collider{Radius: 2})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(second)
	})
	w.read(t)

	var ofFirst, ofSecond []heard
	for _, h := range got {
		switch h.e {
		case first:
			ofFirst = append(ofFirst, h)
		case second:
			ofSecond = append(ofSecond, h)
		}
	}
	if len(ofFirst) == 0 || ofFirst[0].kinds != "added+changed" {
		t.Fatalf("records %v, want %v's addition", got, first)
	}
	for _, h := range ofFirst {
		if h.value == 2 {
			t.Fatalf("%v carries %v, the value of %v, which reused its index: %v", first, h.value, second, got)
		}
	}
	expectHeard(t, "the Entity that reused the index", ofSecond, []heard{{second, "removed", 2}})
}

// TestRecordsAreFixedAtRunStart is hooks.md § Records are history, for a System
// that reads Hooks and acts on the same Component: what it does during its run
// changes no record it was given, and appears in its next run.
func TestRecordsAreFixedAtRunStart(t *testing.T) {
	var got []heard
	var a, b Entity
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedRemoved], set *Set[collider], remove *Remove[collider]) {
		got = got[:0]
		remove.From(a)
		set.UpdateFor(b, collider{Radius: 9})
		listen(h, &got, radius)
	})
	a, b = w.entities.alloc(), w.entities.alloc()

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.UpdateFor(a, collider{Radius: 1})
	})
	w.read(t)
	expectHeard(t, "the run whose own acts happen during it", got, []heard{{a, "added+changed", 1}})

	w.read(t)
	expectHeard(t, "the next run", got, []heard{{a, "removed", 1}, {b, "added+changed", 9}})
}

// namedComponent is a Component holding a string, registered with a writer by
// the plugin that owns it.
type namedComponent struct{ Name string }

type hookOwnerActCmd kernel.Command[hookRequest, hookResponse]

// hookOwnerPlugin owns namedComponent and handles its own writer in its own
// Register, which the kernel runs before any plugin depending on it.
type hookOwnerPlugin struct {
	store *Store[namedComponent]
	act   func(set *Set[namedComponent], remove *Remove[namedComponent])
}

func (p *hookOwnerPlugin) Name() kernel.PluginName { return "hookowner" }

func (p *hookOwnerPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *hookOwnerPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.store = RegisterComponent[namedComponent](registrar, 16)
	registrar.HandleCommand[hookOwnerActCmd](ToExecute[hookRequest, hookResponse](registrar,
		func(set *Set[namedComponent], remove *Remove[namedComponent]) { p.act(set, remove) }))
	return nil
}

// newOwnedHookWorld composes the owner beside the systems plugin, which
// depends on it.
func newOwnedHookWorld(t *testing.T, subscribe func(*kernel.Registrar)) (*Entities, *hookOwnerPlugin, *kernel.Engine) {
	t.Helper()
	owner := &hookOwnerPlugin{}
	entities, _, engine := newWorldWith(t, 16, subscribe, []kernel.PluginName{Name, "hookowner"}, owner)
	return entities, owner, engine
}

func (p *hookOwnerPlugin) write(t *testing.T, engine *kernel.Engine,
	act func(set *Set[namedComponent], remove *Remove[namedComponent]),
) {
	t.Helper()
	p.act = act
	if _, err := engine.Executioner().ExecuteCommand[hookOwnerActCmd](hookRequest{}); err != nil {
		t.Fatalf("running the owner's writer: %v", err)
	}
}

// TestAReaderRegisteredAfterAWriterSeesItsActs is hooks.md § How recording is
// switched on: the owner's writer registers before the dependent plugin's
// reader, and still records for it, because it reads the watched kinds when its
// run starts rather than when it registers.
func TestAReaderRegisteredAfterAWriterSeesItsActs(t *testing.T) {
	var names []string
	entities, owner, engine := newOwnedHookWorld(t, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[namedComponent, HookAddedRemoved]) {
				for e, hook := range h.All() {
					names = append(names, fmt.Sprintf("%v added=%v %s", e, hook.IsAdded(), hook.Value.Name))
				}
			}))
	})
	e := entities.alloc()

	owner.write(t, engine, func(set *Set[namedComponent], remove *Remove[namedComponent]) {
		set.UpdateFor(e, namedComponent{Name: "first"})
		remove.From(e)
	})
	if _, err := engine.Executioner().ExecuteCommand[hookReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}

	want := []string{fmt.Sprintf("%v added=true first", e), fmt.Sprintf("%v added=false first", e)}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("the reader heard %v, want %v", names, want)
	}
}

type hookGuardSystem kernel.Subscription[app.UpdateEvent]

// TestHooksOverAnUnregisteredComponentFailsComposition names the Component, for
// a type no plugin registered and for a pointer to a registered one.
func TestHooksOverAnUnregisteredComponentFailsComposition(t *testing.T) {
	for _, arm := range []struct {
		system any
		named  string
	}{
		{func(h *Hooks[guarded, HookAdded]) {}, "ecs.guarded"},
		{func(h *Hooks[*collider, HookAdded]) {}, "*ecs.collider"},
	} {
		var failure error
		kernel.New(nil).
			Handler(func(err error) bool { failure = err; return true }).
			WithPlugins(
				authority{ids: 8},
				&componentsPlugin{ids: 8},
				&systemsPlugin{subscribe: func(registrar *kernel.Registrar) {
					registrar.Subscribe[hookGuardSystem](ToHandler[app.UpdateEvent](registrar, arm.system))
				}},
			)
		if failure == nil {
			t.Fatalf("composing Hooks over %s succeeded", arm.named)
		}
		for _, want := range []string{"systems", "Hooks[", "unregistered Component " + arm.named} {
			if !strings.Contains(failure.Error(), want) {
				t.Fatalf("composition failure %q does not name %q", failure.Error(), want)
			}
		}
	}
}

// The parallelism guard.

type (
	lockSetGetSystem     kernel.Subscription[app.UpdateEvent]
	lockSetSpawnSystem   kernel.Subscription[app.UpdateEvent]
	lockSetDespawnSystem kernel.Subscription[app.UpdateEvent]
)

// readerOf is an empty System reading collider's Hooks under K.
func readerOf[K KindSet]() any { return func(h *Hooks[collider, K]) {} }

// TestAHookNeverWidensALockSet is hooks.md's own requirement, held exactly: for
// each kind set, every other handler's Reads and Writes are what they are in the
// same world without the reader, and the reader declares a Query's lock set
// over collider and nothing else. The other handlers are every kind of writer
// of collider there is, and one that shares no Store with it.
func TestAHookNeverWidensALockSet(t *testing.T) {
	type lockSet struct{ reads, writes []string }
	describe := func(reader any) (others map[string]lockSet, own *lockSet) {
		_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
			subscribeChurnAndMove(registrar)
			registrar.Subscribe[lockSetGetSystem](ToHandler[app.UpdateEvent](registrar, func(g *Get[collider]) {}))
			registrar.Subscribe[lockSetSpawnSystem](ToHandler[app.UpdateEvent](registrar, func(sp *Spawn[wideSet]) {}))
			registrar.Subscribe[lockSetDespawnSystem](ToHandler[app.UpdateEvent](registrar, func(we *WriteableEntities) {}))
			if reader != nil {
				registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, reader))
			}
		})
		names := func(types []reflect.Type) []string {
			out := make([]string, len(types))
			for i, t := range types {
				out[i] = kernel.TypeName(t)
			}
			slices.Sort(out)
			return out
		}
		others = map[string]lockSet{}
		for _, sub := range engine.Describe().Subscriptions {
			set := lockSet{names(sub.Reads), names(sub.Writes)}
			if sub.Type == reflect.TypeFor[hookReaderSystem]() {
				own = &set
				continue
			}
			others[kernel.TypeName(sub.Type)] = set
		}
		return others, own
	}

	without, _ := describe(nil)
	if len(without) != 5 {
		t.Fatalf("the world without a reader describes %d handlers, want 5: %v", len(without), without)
	}
	for _, arm := range []struct {
		kinds  string
		reader any
	}{
		{"HookSpawned", readerOf[HookSpawned]()},
		{"HookDespawned", readerOf[HookDespawned]()},
		{"HookSpawnedDespawned", readerOf[HookSpawnedDespawned]()},
		{"HookAdded", readerOf[HookAdded]()},
		{"HookRemoved", readerOf[HookRemoved]()},
		{"HookAddedRemoved", readerOf[HookAddedRemoved]()},
		{"HookAddedChanged", readerOf[HookAddedChanged]()},
		{"HookAll", readerOf[HookAll]()},
	} {
		with, own := describe(arm.reader)
		if fmt.Sprint(with) != fmt.Sprint(without) {
			t.Fatalf("a Hooks[collider, %s] reader changed the other handlers' lock sets:\nwithout %v\n   with %v",
				arm.kinds, without, with)
		}
		want := lockSet{
			reads: []string{kernel.TypeName(entitiesType), kernel.TypeName(reflect.TypeFor[*Store[collider]]())},
		}
		slices.Sort(want.reads)
		if own == nil || fmt.Sprint(*own) != fmt.Sprint(want) {
			t.Fatalf("a Hooks[collider, %s] reader declares %v, want %v", arm.kinds, own, want)
		}
	}
}

// TestAHookReaderCostsNoParallelism is the occupancy half of the guard: churn,
// writing collider, and move, writing body, are inside their bodies at once
// with a Hooks[collider, HookAddedRemoved] reader registered. The control is the
// same pair with churn also spawning, which must reach 1, or the 2 proves
// nothing.
//
// The reader runs After churn, as a reader keeping pace with its writer is
// declared. Unordered, any handler reading collider — a Get[collider] as much
// as a Hooks — can reach the kernel's queue while churn holds collider, and the
// queue's conflict-aware FIFO then holds move behind it on their shared
// read{*Entities} until churn ends. That is the kernel's dispatch order, which
// this test does not measure, and not a lock a Hook added.
func TestAHookReaderCostsNoParallelism(t *testing.T) {
	const wait = 100 * time.Millisecond
	measure := func(spawning bool) int32 {
		seen := &occupancy{}
		var churning any = func(set *Set[collider], remove *Remove[collider], q *Query[walkQuery]) {
			seen.overlap(wait)
		}
		if spawning {
			churning = func(set *Set[collider], remove *Remove[collider], q *Query[walkQuery], sp *Spawn[spawnSet]) {
				seen.overlap(wait)
			}
		}
		_, _, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
			registrar.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](registrar, churning))
			registrar.Subscribe[hookMoveSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[moveQuery]) {
				seen.overlap(wait)
			}))
			registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar,
				func(h *Hooks[collider, HookAddedRemoved]) {})).After[hookChurnSystem]()
		})
		frame(t, engine, 1)
		return seen.most.Load()
	}

	if parallel := measure(false); parallel != 2 {
		t.Fatalf("churn and move with a Hooks reader registered reached occupancy %d, want 2", parallel)
	}
	if barrier := measure(true); barrier != 1 {
		t.Fatalf("the control, churn spawning, reached occupancy %d, want 1: without it the 2 proves nothing", barrier)
	}
}

// Counts.

type hookCaptureSystem kernel.Subscription[app.UpdateEvent]

// TestRecordingAnActAllocatesNothing is the accessors on a watched Store, after
// the log has grown to what they fill between two runs of its reader: an
// UpdateFor that adds and a Remove.From allocate nothing, and both are recorded.
func TestRecordingAnActAllocatesNothing(t *testing.T) {
	var set *Set[collider]
	var remove *Remove[collider]
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(s *Set[collider], r *Remove[collider]) {
			set, remove = s, r
		}))
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func(h *Hooks[collider, HookAll]) {}))
	})
	frame(t, engine, 1)
	ids := make([]Entity, 64)
	for i := range ids {
		ids[i] = entities.alloc()
	}
	i := 0
	pair := func() {
		i = (i + 1) % len(ids)
		set.UpdateFor(ids[i], collider{Radius: 1})
		remove.From(ids[i])
	}
	for range 4096 {
		pair()
	}
	frame(t, engine, 1)

	log := components.colliders.hooks
	if objects := testing.AllocsPerRun(1000, pair); objects != 0 {
		t.Fatalf("an UpdateFor and a From on a watched Store allocated %v objects, want 0", objects)
	}
	if len(log.records) != 2*1001 || len(log.retained) != 1001 {
		t.Fatalf("the log holds %d records and %d values after 1001 pairs, want 2002 and 1001",
			len(log.records), len(log.retained))
	}
}

// TestRecordingASpawnAndADespawnAllocatesNothing is Spawn and Despawn on watched
// Stores, after the logs have grown to what they fill between two runs of their
// readers: a Spawn carrying body and velocity and its Despawn allocate nothing,
// and each is recorded once on each Store.
func TestRecordingASpawnAndADespawnAllocatesNothing(t *testing.T) {
	var spawn *Spawn[spawnSet]
	var writeable *WriteableEntities
	_, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(sp *Spawn[spawnSet], we *WriteableEntities) {
			spawn, writeable = sp, we
		}))
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar,
			func(b *Hooks[body, HookSpawnedDespawned], v *Hooks[velocity, HookAll]) {}))
	})
	frame(t, engine, 1)
	values := spawnSet{Body: body{X: 1}, Velocity: velocity{X: 2}}
	pair := func() { writeable.Despawn(spawn.New(values)) }
	for range 4096 {
		pair()
	}
	frame(t, engine, 1)

	if objects := testing.AllocsPerRun(1000, pair); objects != 0 {
		t.Fatalf("a Spawn and a Despawn on watched Stores allocated %v objects, want 0", objects)
	}
	for _, log := range []struct {
		name              string
		records, retained int
	}{
		{"body", len(components.bodies.hooks.records), len(components.bodies.hooks.retained)},
		{"velocity", len(components.velocities.hooks.records), len(components.velocities.hooks.retained)},
	} {
		if log.records != 2*1001 || log.retained != 1001 {
			t.Fatalf("%s's log holds %d records and %d values after 1001 pairs, want 2002 and 1001",
				log.name, log.records, log.retained)
		}
	}
}

type hookStalledCmd kernel.Command[hookRequest, hookResponse]

// TestAStalledReaderHoldsExactlyWhatWasActedSince pins a record at 24 B, and
// counts what a reader that never runs keeps in the log: after k additions and r
// removals, exactly k + r records and r retained values, however often the
// Store's other reader runs.
func TestAStalledReaderHoldsExactlyWhatWasActedSince(t *testing.T) {
	if size := unsafe.Sizeof(hookRecord{}); size != 24 {
		t.Fatalf("a record is %d B, want 24", size)
	}
	const k, r = 7, 3
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedRemoved]) {}, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookStalledCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[collider, HookRemoved]) {}))
	})
	held := make([]Entity, r)
	for i := range held {
		held[i] = w.entities.alloc()
		w.components.colliders.Set(held[i], collider{Radius: 1})
	}
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		for range k {
			set.UpdateFor(w.entities.alloc(), collider{Radius: 2})
		}
		for _, e := range held {
			remove.From(e)
		}
	})
	for range 3 {
		w.read(t)
		log := w.components.colliders.hooks
		if len(log.records) != k+r || len(log.retained) != r {
			t.Fatalf("the log holds %d records and %d values, want %d and %d",
				len(log.records), len(log.retained), k+r, r)
		}
	}
}

type (
	hookFirstReadCmd kernel.Command[hookRequest, hookResponse]
	hookLastReadCmd  kernel.Command[hookRequest, hookResponse]
)

// TestARemovedValueLivesUntilTheLastReaderPassesIt is hooks.md § The log, and
// the locks it is appended under: a removal's copy of T, and the string it
// holds, is reachable until the run end of the last reader of the Store whose
// copy included the record, and no longer. A string is used rather than a List
// because validation mode's stamp table keeps a List's array alive past the
// log, so the claim holds in both build modes.
func TestARemovedValueLivesUntilTheLastReaderPassesIt(t *testing.T) {
	released := make(chan struct{}, 1)
	collected := func(gcs int) bool {
		for range gcs {
			runtime.GC()
			for deadline := time.Now().Add(50 * time.Millisecond); time.Now().Before(deadline); {
				select {
				case <-released:
					return true
				default:
					runtime.Gosched()
				}
			}
		}
		return false
	}
	var insideLast bool
	read := func(h *Hooks[namedComponent, HookAddedRemoved]) {
		for _, hook := range h.All() {
			if !strings.HasPrefix(hook.Value.Name, "a name nobody else holds") {
				t.Errorf("a record carries %q", hook.Value.Name)
			}
		}
	}
	entities, owner, engine := newOwnedHookWorld(t, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookFirstReadCmd](ToExecute[hookRequest, hookResponse](registrar, read))
		registrar.HandleCommand[hookLastReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[namedComponent, HookRemoved]) {
				insideLast = !collected(2)
			}))
	})
	e := entities.alloc()

	owner.write(t, engine, func(set *Set[namedComponent], remove *Remove[namedComponent]) {
		bytes := []byte("a name nobody else holds, " + e.String())
		runtime.AddCleanup(&bytes[0], func(struct{}) { released <- struct{}{} }, struct{}{})
		set.UpdateFor(e, namedComponent{Name: unsafe.String(&bytes[0], len(bytes))})
		remove.From(e)
	})
	if collected(3) {
		t.Fatal("the string was released while no reader had run")
	}
	if _, err := engine.Executioner().ExecuteCommand[hookFirstReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the first reader: %v", err)
	}
	if collected(3) {
		t.Fatal("the string was released after the first reader, while the last had not run")
	}
	if _, err := engine.Executioner().ExecuteCommand[hookLastReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the last reader: %v", err)
	}
	if !insideLast {
		t.Fatal("the string was released during the last reader's run")
	}
	if !collected(2) {
		t.Fatal("the string outlived the last reader's run end by more than 2 GCs")
	}
	runtime.KeepAlive(owner.store)
}
