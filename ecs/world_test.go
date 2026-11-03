package ecs

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

func TestEntityLayout(t *testing.T) {
	e := makeEntity(7, 3)
	if e.index() != 7 || e.generation() != 3 {
		t.Fatalf("entity holds index %d generation %d, want 7 and 3", e.index(), e.generation())
	}
	if nextGeneration(^uint32(0)) != 1 {
		t.Fatal("a wrapping generation does not skip 0")
	}
}

func TestMaskBits(t *testing.T) {
	var m game
	maskSet(&m, 3)
	maskSet(&m, 100)
	maskUnset(&m, 3)
	if maskHas(&m, 3) || !maskHas(&m, 100) || m[1] != 1<<36 {
		t.Fatalf("mask is %b after setting 3 and 100 and unsetting 3", m)
	}
	var spread game
	for _, id := range []ComponentID{127, 0, 64, 63} {
		maskSet(&spread, id)
	}
	if got := slices.Collect(maskIDs(&spread)); !slices.Equal(got, []ComponentID{0, 63, 64, 127}) {
		t.Fatalf("maskIDs yielded %v, want 0 63 64 127", got)
	}
	for id := range maskIDs(&spread) {
		if id != 0 {
			t.Fatalf("maskIDs yielded %d after the loop broke", id)
		}
		break
	}
	if maskBits[game]() != 128 {
		t.Fatalf("game holds %d component types, want 128", maskBits[game]())
	}
}

func TestSpawnHandsOutTheAllocationRange(t *testing.T) {
	type small [1]uint64
	var w World[small]
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	_, err := s.Spawn(nil, 1, NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
		RegisterWorld[small](c, WorldOptions{FirstIndex: 10, LastIndex: 12})
		w = declare[World[small]](c)
	}})
	if err != nil {
		t.Fatal(err)
	}
	for want := uint32(10); want <= 12; want++ {
		if e := w.Spawn(); e.index() != want || e.generation() != 1 || !w.Alive(e) {
			t.Fatalf("Spawn answered index %d generation %d, want %d and 1, alive", e.index(), e.generation(), want)
		}
	}
	if e := w.Spawn(); e != NoEntity {
		t.Fatalf("Spawn past the range answered %#x, want NoEntity", uint64(e))
	}
}

func TestCreateAtPlacesOutsideTheRange(t *testing.T) {
	var w World[game]
	spawn(t, func(c *kernel.Configurator) { w = declare[World[game]](c) })
	mirrored := makeEntity(1<<30, 5)
	if err := w.CreateAt(mirrored); err != nil || !w.Alive(mirrored) {
		t.Fatalf("CreateAt failed: %v", err)
	}
	if err := w.CreateAt(makeEntity(1<<30, 6)); err == nil {
		t.Fatal("CreateAt placed an entity where one is alive")
	}
	if err := w.CreateAt(makeEntity(3, 1)); err == nil {
		t.Fatal("CreateAt placed an entity inside the allocation range")
	}
	if err := w.CreateAt(makeEntity(1<<29, 0)); err == nil {
		t.Fatal("CreateAt placed an entity of generation 0")
	}
	if w.Alive(makeEntity(1<<30, 4)) || w.Alive(NoEntity) {
		t.Fatal("a stale generation or NoEntity is alive")
	}
}

func TestDespawnRemovesEveryComponent(t *testing.T) {
	var w World[game]
	var positions Store[game, position]
	var tags Store[game, frozen]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c, "position")
		RegisterComponent[game, frozen](c, "frozen")
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
		tags = declare[Store[game, frozen]](c)
	})
	a := w.Spawn()
	b := w.Spawn()
	positions.Add(a, position{X: 1})
	positions.Add(b, position{X: 2})
	tags.Add(a, frozen{})
	if !w.Despawn(a) || w.Despawn(a) || w.Alive(a) {
		t.Fatal("Despawn did not end a, or ended it twice")
	}
	if positions.Has(a) || tags.Has(a) || positions.Get(b).X != 2 {
		t.Fatal("Despawn left a's components or disturbed b's")
	}
	if positions.Add(a, position{}) {
		t.Fatal("Add on a despawned entity succeeded")
	}
	if c := w.Spawn(); c != makeEntity(a.index(), 2) {
		t.Fatalf("Spawn answered %#x, want a's index reused at once, as nothing observes", uint64(c))
	}
}

func TestRegisterWorldCompositionErrors(t *testing.T) {
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	spawnWith := func(plugins ...kernel.Plugin) { s.Spawn(nil, 1, plugins...) }
	expectPanic(t, "before the ecs plugin", func() {
		spawnWith(testPlugin{init: func(c *kernel.Configurator) { RegisterWorld[game](c, WorldOptions{}) }})
	})
	expectPanic(t, "registered twice", func() {
		spawnWith(NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
			RegisterWorld[game](c, WorldOptions{})
			RegisterWorld[game](c, WorldOptions{})
		}})
	})
	expectPanic(t, "empty allocation range", func() {
		spawnWith(NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
			RegisterWorld[game](c, WorldOptions{FirstIndex: 5, LastIndex: 4})
		}})
	})
}

// TestWorldAllocatesNothingOnceGrown covers what the other allocation tests
// leave out: spawning and despawning directly, the typed and raw World
// methods, and commits, with observation on and off. Observing, a round also
// abandons reservations, which the reset reclaims; without observation such
// an index is lost, so the round abandons none.
func TestWorldAllocatesNothingOnceGrown(t *testing.T) {
	for _, observing := range []bool{false, true} {
		o := spawnCommands(t)
		if observing {
			ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		}
		w := o.world
		healthID := w.ComponentID[health]()
		var mask game
		maskSet(&mask, healthID)
		values := []byte{1, 0, 0, 0}
		spawned := make([]Entity, 0, 300)
		round := func() {
			spawned = spawned[:0]
			for i := range 100 {
				e := w.Spawn()
				w.Add(e, health{Points: int32(i)})
				w.Write[health](e).Points++
				if !w.Has[health](e) || w.Get[health](e) == nil {
					t.Fatal("a spawned entity lost its health")
				}
				w.WriteRaw(e, healthID)[0]++
				w.AddRaw(e, healthID, w.ReadRaw(e, healthID))
				spawned = append(spawned, e)
			}
			for range 50 {
				spawned = append(spawned, w.SpawnRaw(mask, values))
			}
			for i := range 50 {
				e := o.first.Reserve()
				o.first.Spawn(e)
				o.first.Add(e, health{Points: 1})
				o.first.Set(spawned[i], health{Points: 2})
				o.second.Remove[health](spawned[i+50])
				spawned = append(spawned, e)
				if observing {
					o.second.Reserve() // abandoned, reclaimed by the reset
				}
			}
			o.sink.Commit()
			for _, e := range spawned {
				w.Despawn(e)
			}
			if observing {
				for range o.deltas.All() {
				}
				ObservationResetSystem(nil, kernel.NoArg{}, w)
			}
		}
		round()
		round()
		if got := testing.AllocsPerRun(20, round); got != 0 {
			t.Fatalf("observing %v: a round of spawns, typed and raw access, commits and despawns allocated %v times", observing, got)
		}
	}
}

func TestWorldLookupAgreesWithWorld(t *testing.T) {
	var w World[game]
	var lookup WorldLookup[game]
	var commands CommandBuffer[game, aiCommands]
	var sink CommandSink[game, aiCommands]
	var positionID ComponentID
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		positionID = RegisterComponent[game, position](c, "position")
		RegisterCommandBuffer[game, aiCommands](c)
		w = declare[World[game]](c)
		lookup = declare[WorldLookup[game]](c)
		commands = declare[CommandBuffer[game, aiCommands]](c)
		sink = declare[CommandSink[game, aiCommands]](c)
	})
	e := w.Spawn()
	reserved := commands.Reserve()
	if !lookup.Alive(e) || lookup.Alive(reserved) || lookup.Alive(NoEntity) {
		t.Fatal("the lookup reads a spawned entity as dead, or a reservation or NoEntity as alive")
	}
	w.Despawn(e)
	reused := w.Spawn()
	if reused.index() != e.index() || lookup.Alive(e) || !lookup.Alive(reused) {
		t.Fatalf("%v reused the index of %v, or the lookup reads the dead one alive or the new one dead", reused, e)
	}
	commands.Spawn(reserved)
	sink.Commit()
	if !lookup.Alive(reserved) {
		t.Fatal("the lookup reads a committed reservation as dead")
	}
	if lookup.ComponentID[position]() != positionID || !reflect.DeepEqual(lookup.Schema(), w.Schema()) {
		t.Fatal("the lookup describes the components otherwise than the world")
	}
}

type target struct{ Entity Entity }

// huntCounts holds what each hunt counted, by the hunt's arg.
var huntCounts [2]int

// hunt counts the hunters whose target is alive into huntCounts[number].
func hunt(k *kernel.Kernel, number int, world WorldLookup[game], q Query1[game, target]) kernel.NoResult {
	huntCounts[number] = 0
	for _, row := range q.All() {
		if world.Alive(row.Get().Entity) {
			huntCounts[number]++
		}
	}
	return kernel.NoResult{}
}

// reserveSpawns records spawning 50 reserved entities.
func reserveSpawns(k *kernel.Kernel, _ kernel.NoArg, commands CommandBuffer[game, aiCommands]) kernel.NoResult {
	for range 50 {
		commands.Spawn(commands.Reserve())
	}
	return kernel.NoResult{}
}

// TestWorldLookupsRunAlongsideWriters runs two hunts, each checking its
// targets alive through a WorldLookup, in parallel with a writer toggling a
// component on the targets and a producer reserving entities, and despawns
// targets between rounds. Run under -race, it checks that a world read sees
// nothing those writers change.
func TestWorldLookupsRunAlongsideWriters(t *testing.T) {
	var hunts []*kernel.Task[int, kernel.NoResult]
	var toggleTask, reserveTask, commitTask *kernel.Task[kernel.NoArg, kernel.NoResult]
	var w World[game]
	var targets Store[game, target]
	h, prey := queryWorld(t, 300, func(c *kernel.Configurator) {
		RegisterComponent[game, target](c, "target")
		RegisterCommandBuffer[game, aiCommands](c)
		hunts = append(hunts, c.RegisterTask2(hunt), c.RegisterTask2(hunt))
		toggleTask = c.RegisterTask2(toggle[velocity])
		reserveTask = c.RegisterTask1(reserveSpawns)
		commitTask = c.RegisterTask1(commit)
		w = declare[World[game]](c)
		targets = declare[Store[game, target]](c)
	})
	for _, e := range prey {
		targets.Add(w.Spawn(), target{Entity: e})
	}
	alive := len(prey)
	for round := range 5 {
		h.run(func(ex *kernel.Executor) kernel.JobHandle {
			var jobs []kernel.JobHandle
			for i, task := range hunts {
				jobs = append(jobs, ex.Schedule(task, i).Handle())
			}
			jobs = append(jobs, ex.Schedule(toggleTask, kernel.NoArg{}).Handle(), ex.Schedule(reserveTask, kernel.NoArg{}).Handle())
			return ex.Schedule(commitTask, kernel.NoArg{}, jobs...).Handle()
		})
		for i, count := range huntCounts {
			if count != alive {
				t.Fatalf("round %d: hunt %d counted %d live targets, want %d", round, i, count, alive)
			}
		}
		for i := round; i < len(prey); i += 7 {
			if w.Despawn(prey[i]) {
				alive--
			}
		}
	}
}

// shrinkWorld is a world with an observed store, an unobserved one, refs, a
// command buffer with a lane of each kind, and its sink.
type shrinkWorld struct {
	world       World[game]
	healths     Store[game, health]
	positions   Store[game, position]
	deltas      DeltaQuery1[game, health]
	refs        Refs[game, string]
	commands    CommandBuffer[game, aiCommands]
	commandRefs CommandRefs[game, aiCommands, string]
	sink        CommandSink[game, aiCommands]
}

func spawnShrink(t testing.TB) *shrinkWorld {
	o := &shrinkWorld{}
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		RegisterComponent[game, position](c, "position")
		RegisterRefs[game, string](c, "string", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		o.world = declare[World[game]](c)
		o.healths = declare[Store[game, health]](c)
		o.positions = declare[Store[game, position]](c)
		o.deltas = declare[DeltaQuery1[game, health]](c)
		o.refs = declare[Refs[game, string]](c)
		o.commands = declare[CommandBuffer[game, aiCommands]](c)
		o.commandRefs = declare[CommandRefs[game, aiCommands, string]](c)
		o.sink = declare[CommandSink[game, aiCommands]](c)
	})
	return o
}

// TestShrinkKeepsWhatIsInUse grows a world's entities, stores, refs and
// lanes, lets most of it go, leaves records waiting and shrinks, in a world
// that observes and in one that does not. What is alive and recorded reads
// the same, the chunks and index pages past it are released, and the world
// observes as before, its window ended.
func TestShrinkKeepsWhatIsInUse(t *testing.T) {
	for _, observing := range []bool{false, true} {
		o := spawnShrink(t)
		if observing {
			ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		}
		var entities []Entity
		var names []Ref[string]
		for i := range 10_000 {
			e := o.world.Spawn()
			entities = append(entities, e)
			o.healths.Add(e, health{Points: int32(i)})
			o.positions.Add(e, position{X: float32(i)})
			names = append(names, o.refs.New(fmt.Sprint(i)))
			o.commands.Set(e, health{Points: int32(i)})
		}
		o.sink.Commit()
		kept := map[Entity]int{}
		for i, e := range entities {
			if (i < indexPageSize && i%6 == 0) || i == len(entities)-1 {
				kept[e] = i
				continue
			}
			o.world.Despawn(e)
			o.refs.Release(names[i])
		}
		pending := o.commands.Reserve()
		o.commands.Spawn(pending)
		o.commands.Add(pending, health{Points: -1})
		pendingName := o.commandRefs.New("pending")
		o.world.Shrink()
		for _, s := range []*storeResource{o.healths.store, o.positions.store} {
			pages := 0
			for _, page := range s.rowByIndex.pages {
				if page != nil {
					pages++
				}
			}
			chunks := (s.len() + rowMask) >> rowShift
			if len(s.valueChunks) != chunks || (s.observed() && len(s.shadowChunks) != chunks) || pages != 2 {
				t.Fatalf("observing %v: a store of %d rows kept %d value chunks, %d shadow chunks and %d index pages, want %d, %d and 2: the first and the last",
					observing, s.len(), len(s.valueChunks), len(s.shadowChunks), pages, chunks, chunks)
			}
		}
		if o.world.world.entities.observing != observing || o.healths.store.observed() != observing {
			t.Fatalf("observing %v: Shrink changed whether the world observes", observing)
		}
		if deltas := collectDeltas(o.deltas); len(deltas) != 0 {
			t.Fatalf("observing %v: Shrink left %d deltas in the window", observing, len(deltas))
		}
		checkKept := func() {
			for e, i := range kept {
				if !o.world.Alive(e) || o.healths.Get(e).Points != int32(i) || o.positions.Get(e).X != float32(i) || *o.refs.Get(names[i]) != fmt.Sprint(i) {
					t.Fatalf("observing %v: entity %#x lost what it holds", observing, uint64(e))
				}
			}
		}
		checkKept()
		// The reset a shrink makes in a world that observes takes back the
		// reservation, as ObservationResetSystem does: its Spawn and Add drop.
		wantDropped := 0
		if observing {
			wantDropped = 2
		}
		if dropped := o.sink.Commit(); dropped != wantDropped || o.world.Alive(pending) == observing {
			t.Fatalf("observing %v: the commit after Shrink dropped %d records, want %d", observing, dropped, wantDropped)
		}
		if name := o.refs.Get(pendingName); name == nil || *name != "pending" {
			t.Fatalf("observing %v: a ref recorded before Shrink reads %v", observing, name)
		}
		for range 10_000 {
			e := o.world.Spawn()
			o.healths.Add(e, health{Points: -1})
			o.positions.Add(e, position{X: -1})
			o.refs.New("again")
		}
		checkKept()
	}
}

func TestShrinkAllocatesNothingOnceRegrown(t *testing.T) {
	o := spawnShrink(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	entities := make([]Entity, 1000)
	names := make([]Ref[string], len(entities))
	round := func() {
		for i := range entities {
			entities[i] = o.world.Spawn()
			o.healths.Add(entities[i], health{Points: int32(i)})
			o.positions.Add(entities[i], position{X: float32(i)})
			names[i] = o.refs.New("name")
			o.commands.Set(entities[i], health{})
		}
		o.sink.Commit()
		for i, e := range entities {
			o.world.Despawn(e)
			o.refs.Release(names[i])
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	round()
	o.world.Shrink()
	round()
	if got := testing.AllocsPerRun(20, round); got != 0 {
		t.Fatalf("a round after Shrink and regrowing allocated %v times", got)
	}
}

// TestShrinkReleasesRefsAndLanes grows the free refs, a lane's records and
// values and a refs lane's records, empties them, and shrinks: growing each
// again allocates, where it allocated nothing before the shrink. Each is
// regrown after the ones its cycle also grows.
func TestShrinkReleasesRefsAndLanes(t *testing.T) {
	o := spawnShrink(t)
	e := o.world.Spawn()
	names := make([]Ref[string], 10_000)
	for i := range names {
		names[i] = o.refs.New("name")
	}
	cycles := []struct {
		name  string
		cycle func()
	}{
		{"the free refs", func() {
			for _, h := range names {
				o.refs.Release(h)
			}
			for i := range names {
				names[i] = o.refs.New("name")
			}
		}},
		{"a lane's records", func() {
			for range names {
				o.commands.Remove[health](e)
			}
			o.sink.Commit()
		}},
		{"a lane's values", func() {
			for range names {
				o.commands.Set(e, health{})
			}
			o.sink.Commit()
		}},
		{"a refs lane's records", func() {
			for range names {
				o.commandRefs.Release(Ref[string]{})
			}
			o.sink.Commit()
		}},
	}
	for _, c := range cycles {
		if got := testing.AllocsPerRun(5, c.cycle); got != 0 {
			t.Fatalf("growing %s allocated %v times before the shrink", c.name, got)
		}
	}
	o.world.Shrink()
	for _, c := range cycles {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		c.cycle()
		runtime.ReadMemStats(&after)
		if after.Mallocs == before.Mallocs {
			t.Fatalf("growing %s after Shrink allocated nothing: its chunks were kept", c.name)
		}
	}
}

// spawnFrozen records spawning 50 entities, each frozen.
func spawnFrozen(k *kernel.Kernel, _ kernel.NoArg, commands CommandBuffer[game, aiCommands]) kernel.NoResult {
	for range 50 {
		e := commands.Reserve()
		commands.Spawn(e)
		commands.Add(e, frozen{})
	}
	return kernel.NoResult{}
}

// churnNames makes 50 refs and releases them.
func churnNames(k *kernel.Kernel, _ kernel.NoArg, refs Refs[game, string]) kernel.NoResult {
	var handles [50]Ref[string]
	for i := range handles {
		handles[i] = refs.New("name")
	}
	for _, h := range handles {
		refs.Release(h)
	}
	return kernel.NoResult{}
}

// recordNames records making 10 refs.
func recordNames(k *kernel.Kernel, _ kernel.NoArg, commandRefs CommandRefs[game, aiCommands, string]) kernel.NoResult {
	for range 10 {
		commandRefs.New("recorded")
	}
	return kernel.NoResult{}
}

// TestShrinkRunsApartFromWorldResources schedules the Shrink system with
// nothing between it and systems writing refs, recording commands and
// recording refs, and commits after them all. Run under -race, it checks
// that the barrier keeps the refs and the lanes a shrink reaches apart from
// their writers.
func TestShrinkRunsApartFromWorldResources(t *testing.T) {
	var shrinkTask, spawnTask, churnTask, recordTask, commitTask *kernel.Task[kernel.NoArg, kernel.NoResult]
	var tags Store[game, frozen]
	h, _ := queryWorld(t, 300, func(c *kernel.Configurator) {
		RegisterRefs[game, string](c, "string", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		shrinkTask = c.RegisterTask1(ShrinkSystem[game])
		spawnTask = c.RegisterTask1(spawnFrozen)
		churnTask = c.RegisterTask1(churnNames)
		recordTask = c.RegisterTask1(recordNames)
		commitTask = c.RegisterTask1(commit)
		tags = declare[Store[game, frozen]](c)
	})
	const rounds = 5
	for range rounds {
		h.run(func(ex *kernel.Executor) kernel.JobHandle {
			jobs := []kernel.JobHandle{
				ex.Schedule(shrinkTask, kernel.NoArg{}).Handle(),
				ex.Schedule(spawnTask, kernel.NoArg{}).Handle(),
				ex.Schedule(churnTask, kernel.NoArg{}).Handle(),
				ex.Schedule(recordTask, kernel.NoArg{}).Handle(),
			}
			return ex.Schedule(commitTask, kernel.NoArg{}, jobs...).Handle()
		})
	}
	if got, want := tags.store.len(), 100+50*rounds; got != want {
		t.Fatalf("%d entities are frozen, want %d", got, want)
	}
}
