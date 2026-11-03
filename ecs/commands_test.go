package ecs

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type aiCommands struct{}

// commandWorld is a world with a command buffer, two producer lanes, its
// sink, and handles to look at the result.
type commandWorld struct {
	world   World[game]
	first   CommandBuffer[game, aiCommands]
	second  CommandBuffer[game, aiCommands]
	sink    CommandSink[game, aiCommands]
	healths Store[game, health]
	deltas  DeltaQuery1[game, health]
}

func spawnCommands(t *testing.T) *commandWorld {
	o := &commandWorld{}
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		RegisterCommandBuffer[game, aiCommands](c)
		o.world = declare[World[game]](c)
		o.first = declare[CommandBuffer[game, aiCommands]](c)
		o.second = declare[CommandBuffer[game, aiCommands]](c)
		o.sink = declare[CommandSink[game, aiCommands]](c)
		o.healths = declare[Store[game, health]](c)
		o.deltas = declare[DeltaQuery1[game, health]](c)
	})
	return o
}

func TestCommitSpawnsReservedEntities(t *testing.T) {
	o := spawnCommands(t)
	e := o.first.Reserve()
	o.first.Spawn(e)
	o.first.Add(e, health{Points: 5})
	if o.world.Alive(e) {
		t.Fatal("a reserved entity is alive before its commit")
	}
	if dropped := o.sink.Commit(); dropped != 0 || !o.world.Alive(e) || o.healths.Get(e).Points != 5 {
		t.Fatalf("the commit dropped %d records, or left e dead or without its health", dropped)
	}
	o.first.Spawn(e)
	if o.sink.Commit() != 1 {
		t.Fatal("spawning an entity twice was not dropped")
	}
}

func TestCommitAppliesSpawnsThenDespawnsThenLaneOrder(t *testing.T) {
	o := spawnCommands(t)
	a := o.world.Spawn()
	b := o.world.Spawn()
	o.healths.Add(b, health{Points: 1})
	o.first.Add(a, health{Points: 1}) // a is despawned before this applies
	o.second.Despawn(a)
	o.first.Set(b, health{Points: 2})
	o.second.Set(b, health{Points: 3}) // the later lane wins
	if dropped := o.sink.Commit(); dropped != 1 || o.world.Alive(a) {
		t.Fatalf("the commit dropped %d records, want the add to despawned a", dropped)
	}
	if got := o.healths.Get(b).Points; got != 3 {
		t.Fatalf("b holds %d, want the later lane's 3", got)
	}
	// A spawn of a later lane comes before an add of an earlier one, and
	// before a despawn recorded with it.
	added := o.second.Reserve()
	o.first.Add(added, health{Points: 4})
	o.second.Spawn(added)
	ended := o.first.Reserve()
	o.first.Spawn(ended)
	o.first.Despawn(ended)
	if dropped := o.sink.Commit(); dropped != 0 {
		t.Fatalf("the commit dropped %d records, want none", dropped)
	}
	if got := o.healths.Get(added); got == nil || got.Points != 4 {
		t.Fatalf("the entity spawned by a later lane holds %v, want the earlier lane's add", got)
	}
	if o.world.Alive(ended) {
		t.Fatal("an entity spawned and despawned in one commit is alive")
	}
}

func TestCommitSetAndRemoveOfAMissingComponentDoNothing(t *testing.T) {
	o := spawnCommands(t)
	e := o.world.Spawn()
	o.first.Set(e, health{Points: 9})
	o.first.Remove[health](e)
	if dropped := o.sink.Commit(); dropped != 0 || o.healths.Has(e) {
		t.Fatalf("the commit dropped %d records, or Set added a component", dropped)
	}
	o.first.Add(e, health{Points: 1})
	o.first.Remove[health](e)
	o.sink.Commit()
	if o.healths.Has(e) {
		t.Fatal("Remove after Add left the component")
	}
	o.world.Despawn(e)
	o.first.Set(e, health{Points: 9})
	o.first.Remove[health](e)
	if dropped := o.sink.Commit(); dropped != 2 {
		t.Fatalf("the commit dropped %d records of a dead entity, want 2", dropped)
	}
}

func TestStaleReservationIsRefusedAfterTheReset(t *testing.T) {
	o := spawnCommands(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world) // observing, so the reset reclaims
	stale := o.first.Reserve()
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.first.Spawn(stale)
	if o.sink.Commit() != 1 || o.world.Alive(stale) {
		t.Fatal("a reservation spawned after the reset that reclaimed it came alive")
	}
	if e := o.world.Spawn(); e.index() != stale.index() || e.generation() == stale.generation() {
		t.Fatalf("Spawn answered %#x, want the stale index at a new generation", uint64(e))
	}
}

func TestObserversSeeCommits(t *testing.T) {
	o := spawnCommands(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	e := o.world.Spawn()
	o.healths.Add(e, health{Points: 1})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.first.Set(e, health{Points: 2})
	spawned := o.first.Reserve()
	o.first.Spawn(spawned)
	o.first.Add(spawned, health{Points: 7})
	o.sink.Commit()
	got := collectDeltas(o.deltas)
	if got[e] != (observedDelta[health]{Delta: DeltaChanged, Value: health{2}, Previous: health{1}}) ||
		got[spawned] != (observedDelta[health]{Delta: DeltaAdded, Value: health{7}}) {
		t.Fatalf("observers saw %+v after the commit", got)
	}
}

func TestRecordingAndCommittingAllocateNothingOnceGrown(t *testing.T) {
	o := spawnCommands(t)
	entities := make([]Entity, 200)
	for i := range entities {
		entities[i] = o.world.Spawn()
	}
	round := func() {
		for _, e := range entities {
			o.first.Add(e, health{Points: 1})
			o.second.Set(e, health{Points: 2})
		}
		for range 50 {
			e := o.first.Reserve()
			o.first.Spawn(e)
			o.second.Despawn(e)
		}
		o.sink.Commit()
	}
	round()
	if got := testing.AllocsPerRun(20, round); got != 0 {
		t.Fatalf("recording and committing allocated %v times", got)
	}
}

func TestRegisterCommandBufferTwicePanics(t *testing.T) {
	expectPanic(t, "registered twice", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterCommandBuffer[game, aiCommands](c)
			RegisterCommandBuffer[game, aiCommands](c)
		})
	})
}

// produce is a producer system: it spawns arg.Count entities whose health
// tells arg.Producer's apart, and despawns every entity whose health is a
// multiple of arg.Producer+2.
type produceArg struct{ Producer, Count, Round int32 }

func produce(k *kernel.Kernel, arg produceArg, cmds CommandBuffer[game, aiCommands], q Query1[game, health]) kernel.NoResult {
	for i := range arg.Count {
		e := cmds.Reserve()
		cmds.Spawn(e)
		cmds.Add(e, health{Points: arg.Round*1_000_000 + arg.Producer*1000 + i})
	}
	for e, row := range q.All() {
		if row.Get().Points%(arg.Producer+2) == 0 {
			cmds.Despawn(e)
		}
	}
	return kernel.NoResult{}
}

func commit(k *kernel.Kernel, _ kernel.NoArg, sink CommandSink[game, aiCommands]) kernel.NoResult {
	sink.Commit()
	return kernel.NoResult{}
}

// runProducers runs rounds of producers, in parallel, each round committed
// after them, with workers workers, and answers the healths left.
func runProducers(t *testing.T, workers int) []int32 {
	const producers, rounds = 6, 5
	var producerTasks []*kernel.Task[produceArg, kernel.NoResult]
	var commitTask *kernel.Task[kernel.NoArg, kernel.NoResult]
	var healths Query1[game, health]
	h := spawnWorkers(t, workers, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		RegisterCommandBuffer[game, aiCommands](c)
		for range producers {
			producerTasks = append(producerTasks, c.RegisterTask2(produce))
		}
		commitTask = c.RegisterTask1(commit)
		healths = declare[Query1[game, health]](c)
	})
	for round := range int32(rounds) {
		h.run(func(ex *kernel.Executor) kernel.JobHandle {
			var produced []kernel.JobHandle
			for producer, task := range producerTasks {
				produced = append(produced, ex.Schedule(task, produceArg{Producer: int32(producer), Count: 40, Round: round}).Handle())
			}
			return ex.Schedule(commitTask, kernel.NoArg{}, produced...).Handle()
		})
	}
	var left []int32
	for _, row := range healths.All() {
		left = append(left, row.Get().Points)
	}
	slices.Sort(left)
	return left
}

func TestCommitsGiveTheSameWorldWithOneWorkerAndMany(t *testing.T) {
	one := runProducers(t, 1)
	many := runProducers(t, 8)
	if len(one) == 0 || len(one) == 6*40*5 || !slices.Equal(one, many) {
		t.Fatalf("one worker left %d healths, eight left %d, or they differ or nothing was despawned", len(one), len(many))
	}
}

func TestTypeErasedAccess(t *testing.T) {
	var w World[game]
	var positions Store[game, position]
	var healthID, positionID ComponentID
	spawn(t, func(c *kernel.Configurator) {
		healthID = RegisterComponent[game, health](c, "health")
		positionID = RegisterComponent[game, position](c, "position")
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
	})
	if schema := w.Schema(); len(schema.Components) != 2 || schema.Components[positionID].Size != 8 {
		t.Fatalf("the registry describes %d components, position %+v", len(schema.Components), schema.Components[positionID])
	}
	var mask game
	maskSet(&mask, healthID)
	maskSet(&mask, positionID)
	values := []byte{7, 0, 0, 0 /* health */, 0, 0, 128, 63, 0, 0, 0, 64 /* position {1, 2} */}
	e := w.SpawnRaw(mask, values)
	if got, _ := w.ComponentMask(e); got != mask || *positions.Get(e) != (position{X: 1, Y: 2}) {
		t.Fatalf("SpawnRaw made an entity of mask %b holding %v", got, *positions.Get(e))
	}
	if got := w.ReadRaw(e, healthID); !slices.Equal(got, []byte{7, 0, 0, 0}) {
		t.Fatalf("ReadRaw answered %v", got)
	}
	w.WriteRaw(e, healthID)[0] = 9
	if !w.AddRaw(e, positionID, []byte{0, 0, 0, 0, 0, 0, 0, 0}) || *positions.Get(e) != (position{}) {
		t.Fatal("Add by id did not replace the position")
	}
	if !w.RemoveRaw(e, healthID) || w.ReadRaw(e, healthID) != nil || w.WriteRaw(e, healthID) != nil {
		t.Fatal("Remove by id left the health")
	}
	if err := w.CreateAtRaw(makeEntity(1<<30, 1), mask, values); err != nil {
		t.Fatal(err)
	}
	expectPanic(t, "bytes of values", func() { w.SpawnRaw(mask, values[:5]) })
	if w.ComponentID[position]() != positionID {
		t.Fatal("ComponentID answered another component's ID")
	}
	expectPanic(t, "adding 3 bytes", func() { w.AddRaw(e, healthID, []byte{1, 2, 3}) })
}

func TestTypedWorldAccess(t *testing.T) {
	var w World[game]
	var deltas DeltaQuery1[game, health]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		w = declare[World[game]](c)
		deltas = declare[DeltaQuery1[game, health]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	e := w.Spawn()
	if !w.Add(e, health{Points: 3}) || !w.Has[health](e) || w.Get[health](e).Points != 3 {
		t.Fatal("Add, Has or Get by type disagree")
	}
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	w.Write[health](e).Points = 4
	reported := 0
	for range deltas.All() {
		reported++
	}
	if reported != 1 {
		t.Fatalf("Write by type reported %d changes, want 1", reported)
	}
	if !w.Remove[health](e) || w.Has[health](e) || w.Get[health](e) != nil {
		t.Fatal("Remove by type left the component")
	}
	expectPanic(t, "not registered", func() { w.Has[position](e) })
}

// commandRefsWorld is refsWorld with a command buffer, two lanes of ref
// records on it, a lane of component records and its sink.
type commandRefsWorld struct {
	*refsWorld
	first, second CommandRefs[game, aiCommands, string]
	commands      CommandBuffer[game, aiCommands]
	sink          CommandSink[game, aiCommands]
}

func spawnCommandRefs(t testing.TB) *commandRefsWorld {
	o := &commandRefsWorld{refsWorld: &refsWorld{}}
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, named](c, "named")
		RegisterRefs[game, string](c, "string", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		o.world = declare[World[game]](c)
		o.store = declare[Store[game, named]](c)
		o.refs = declare[Refs[game, string]](c)
		o.lookup = declare[RefsLookup[game, string]](c)
		o.refDeltas = declare[DeltaRefs[game, string]](c)
		o.first = declare[CommandRefs[game, aiCommands, string]](c)
		o.second = declare[CommandRefs[game, aiCommands, string]](c)
		o.commands = declare[CommandBuffer[game, aiCommands]](c)
		o.sink = declare[CommandSink[game, aiCommands]](c)
	})
	return o
}

// TestCommandRefsMatchAReferenceModel mixes refs made, set and released
// directly and through two lanes of a buffer, committed now and then, and
// checks after every step that live handles read their values and that
// handles waiting for the commit or released read nothing.
func TestCommandRefsMatchAReferenceModel(t *testing.T) {
	o := spawnCommandRefs(t)
	random := rand.New(rand.NewPCG(5, 9))
	model := map[Ref[string]]string{}
	waiting := map[Ref[string]]string{}
	// setting holds the value each lane records for a handle, applied at the
	// commit in lane order.
	setting := [2]map[Ref[string]]string{{}, {}}
	releasing := map[Ref[string]]bool{}
	var released []Ref[string]
	wantDropped := 0
	// settled picks a live handle no set or release is recorded for.
	settled := func() (Ref[string], bool) {
		for h := range model {
			if _, first := setting[0][h]; !releasing[h] && !first {
				if _, second := setting[1][h]; !second {
					return h, true
				}
			}
		}
		return Ref[string]{}, false
	}
	for step := range 5000 {
		value := fmt.Sprint(step)
		laneIndex := random.IntN(2)
		lane := o.first
		if laneIndex == 1 {
			lane = o.second
		}
		h, ok := settled()
		switch op := random.IntN(10); {
		case op == 0 || !ok:
			model[o.refs.New(value)] = value
		case op == 1:
			o.refs.Set(h, value)
			model[h] = value
		case op == 2:
			o.refs.Release(h)
			delete(model, h)
			released = append(released, h)
		case op == 3:
			waiting[lane.New(value)] = value
		case op == 4:
			lane.Set(h, value)
			setting[laneIndex][h] = value
		case op == 5:
			lane.Release(h)
			releasing[h] = true
		case op == 6 && len(released) > 0:
			lane.Release(released[random.IntN(len(released))])
			wantDropped++
		case op == 7 && len(released) > 0:
			lane.Set(released[random.IntN(len(released))], value)
			wantDropped++
		default:
			if dropped := o.sink.Commit(); dropped != wantDropped {
				t.Fatalf("step %d: the commit dropped %d records, want %d", step, dropped, wantDropped)
			}
			wantDropped = 0
			for h := range releasing {
				delete(model, h)
				released = append(released, h)
			}
			clear(releasing)
			for _, sets := range setting {
				for h, value := range sets {
					model[h] = value
				}
				clear(sets)
			}
			for h, value := range waiting {
				if _, ok := model[h]; ok {
					t.Fatalf("step %d: handle %+v was handed out twice", step, h)
				}
				model[h] = value
			}
			clear(waiting)
		}
		for h, want := range model {
			if got := o.lookup.Get(h); got == nil || *got != want {
				t.Fatalf("step %d: handle %+v reads %v, want %q", step, h, got, want)
			}
		}
		for h := range waiting {
			if _, ok := model[h]; ok || o.lookup.Get(h) != nil {
				t.Fatalf("step %d: handle %+v reads before its commit, or is live twice", step, h)
			}
		}
	}
	for _, h := range released {
		if o.lookup.Get(h) != nil {
			t.Fatalf("released handle %+v still reads", h)
		}
	}
}

// TestCommandRefsDieWithABufferedDespawn releases a ref beside the buffered
// despawn of the entity holding it: the ref reads until the commit, and its
// slot is the next one handed out.
func TestCommandRefsDieWithABufferedDespawn(t *testing.T) {
	o := spawnCommandRefs(t)
	e := o.world.Spawn()
	o.store.Add(e, named{Name: o.refs.New("a")})
	name := o.store.Get(e).Name
	o.commands.Despawn(e)
	o.first.Release(name)
	if got := o.lookup.Get(name); got == nil || *got != "a" {
		t.Fatal("a ref recorded for release stopped reading before the commit")
	}
	if dropped := o.sink.Commit(); dropped != 0 || o.world.Alive(e) || o.lookup.Get(name) != nil {
		t.Fatalf("the commit dropped %d records, or left the entity or its ref alive", dropped)
	}
	if next := o.refs.New("b"); next.index != name.index {
		t.Fatalf("New answered slot %d, want the released slot %d", next.index, name.index)
	}
	o.first.Release(name)
	if o.sink.Commit() != 1 {
		t.Fatal("releasing a stale ref was not dropped")
	}
}

// TestCommandRefsSetKeepsTheHandle sets a ref through a buffer, observed:
// the handle reads the value from the commit on, and DeltaRefs reports it.
func TestCommandRefsSetKeepsTheHandle(t *testing.T) {
	o := spawnCommandRefs(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	h := o.refs.New("a")
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.first.Set(h, "b")
	if *o.lookup.Get(h) != "a" {
		t.Fatal("a buffered set applied before the commit")
	}
	o.sink.Commit()
	if got := collectRefDeltas(t, o.refDeltas); *o.lookup.Get(h) != "b" || len(got) != 1 || got[h] != (refDelta[string]{Delta: DeltaChanged, Value: "b"}) {
		t.Fatalf("after the commit h reads %q and DeltaRefs answered %+v, want b changed", *o.lookup.Get(h), got)
	}
}

func TestCommandRefsAllocateNothingOnceGrown(t *testing.T) {
	o := spawnCommandRefs(t)
	handles := make([]Ref[string], 200)
	round := func() {
		for i := range handles {
			handles[i] = o.first.New("a")
		}
		o.sink.Commit()
		for i := range handles {
			o.second.Set(handles[i], "b")
		}
		o.sink.Commit()
		for _, h := range handles {
			if *o.lookup.Get(h) != "b" {
				t.Fatal("a ref did not read what was set")
			}
			o.first.Release(h)
		}
		o.sink.Commit()
	}
	round()
	if got := testing.AllocsPerRun(10, round); got != 0 {
		t.Fatalf("recording and committing refs allocated %v times", got)
	}
}

func TestCommandRefsCompositionErrors(t *testing.T) {
	expectPanic(t, "not initialized", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterCommandBuffer[game, aiCommands](c)
			declare[CommandRefs[game, aiCommands, string]](c)
		})
	})
	expectPanic(t, "not initialized", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, string](c, "string", RefsOptions{})
			declare[CommandRefs[game, aiCommands, string]](c)
		})
	})
	expectPanic(t, "CommandKeyedRefs records them", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterKeyedRefs[game, Text, Text](c, "Text", RefsOptions{})
			RegisterCommandBuffer[game, aiCommands](c)
			declare[CommandRefs[game, aiCommands, Text]](c)
		})
	})
	expectPanic(t, "not keyed", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, Text](c, "Text", RefsOptions{})
			RegisterCommandBuffer[game, aiCommands](c)
			declare[CommandKeyedRefs[game, aiCommands, Text, Text]](c)
		})
	})
}

// TestCommandKeyedRefsUpdateAndRelease records updates and releases of
// keyed texts on two lanes: they apply at the commit in lane order, and
// those whose key or ref is gone are dropped.
func TestCommandKeyedRefsUpdateAndRelease(t *testing.T) {
	var texts KeyedRefs[game, Text, Text]
	var first, second CommandKeyedRefs[game, aiCommands, Text, Text]
	var sink CommandSink[game, aiCommands]
	var deltas DeltaRefs[game, Text]
	var w World[game]
	spawn(t, func(c *kernel.Configurator) {
		RegisterKeyedRefs[game, Text, Text](c, "Text", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		w = declare[World[game]](c)
		texts = declare[KeyedRefs[game, Text, Text]](c)
		first = declare[CommandKeyedRefs[game, aiCommands, Text, Text]](c)
		second = declare[CommandKeyedRefs[game, aiCommands, Text, Text]](c)
		sink = declare[CommandSink[game, aiCommands]](c)
		deltas = declare[DeltaRefs[game, Text]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	a := texts.Put("a")
	b := texts.Put("b")
	c := texts.Put("c")
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	first.Update("a")
	second.ReleaseKey("a") // the later lane releases what the first updated
	first.Release(b)
	second.Release(b)    // b is gone by then
	first.Update("gone") // no ref has this key
	second.ReleaseKey("gone")
	if !texts.Alive(a) || !texts.Alive(b) {
		t.Fatal("a record applied before the commit")
	}
	if dropped := sink.Commit(); dropped != 3 {
		t.Fatalf("the commit dropped %d records, want 3", dropped)
	}
	if texts.Alive(a) || texts.Alive(b) || !texts.Alive(c) {
		t.Fatal("the commit left a or b alive, or released c")
	}
	if _, ok := texts.Find("a"); ok {
		t.Fatal("a released key still finds its ref")
	}
	got := collectRefDeltas(t, deltas)
	if len(got) != 2 || got[a].Delta != DeltaReleased || got[b].Delta != DeltaReleased {
		t.Fatalf("DeltaRefs answered %+v, want a and b released", got)
	}
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	first.Update("c")
	sink.Commit()
	if got := collectRefDeltas(t, deltas); len(got) != 1 || got[c].Delta != DeltaChanged {
		t.Fatalf("DeltaRefs answered %+v after a buffered update, want c changed", got)
	}
}

// nameArg tells nameProducer which names to make: each holds the round, the
// producer and a number of its own.
type nameArg struct{ Producer, Count, Round int }

// nameProducer is a producer system: it despawns, releasing its name, every
// entity whose name is a multiple of arg.Producer+2 long, and spawns
// arg.Count entities, each named by a ref made through the buffer.
func nameProducer(k *kernel.Kernel, arg nameArg, cmds CommandBuffer[game, aiCommands], refs CommandRefs[game, aiCommands, string], lookup RefsLookup[game, string], q Query1[game, named]) kernel.NoResult {
	for e, row := range q.All() {
		name := row.Get().Name
		if len(*lookup.Get(name))%(arg.Producer+2) == 0 {
			cmds.Despawn(e)
			refs.Release(name)
		}
	}
	for i := range arg.Count {
		e := cmds.Reserve()
		cmds.Spawn(e)
		cmds.Add(e, named{Name: refs.New(fmt.Sprintf("%d.%d.%d", arg.Round, arg.Producer, i*i))})
	}
	return kernel.NoResult{}
}

// runNameProducers runs rounds of name producers in parallel, each round
// committed after them, with workers workers, and answers the names left.
// Every ref alive must be named by an entity.
func runNameProducers(t *testing.T, workers int) []string {
	const producers, rounds = 6, 5
	var producerTasks []*kernel.Task[nameArg, kernel.NoResult]
	var commitTask *kernel.Task[kernel.NoArg, kernel.NoResult]
	var names Query1[game, named]
	var lookup RefsLookup[game, string]
	h := spawnWorkers(t, workers, func(c *kernel.Configurator) {
		RegisterComponent[game, named](c, "named")
		RegisterRefs[game, string](c, "string", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		for range producers {
			producerTasks = append(producerTasks, c.RegisterTask4(nameProducer))
		}
		commitTask = c.RegisterTask1(commit)
		names = declare[Query1[game, named]](c)
		lookup = declare[RefsLookup[game, string]](c)
	})
	for round := range rounds {
		h.run(func(ex *kernel.Executor) kernel.JobHandle {
			var produced []kernel.JobHandle
			for producer, task := range producerTasks {
				produced = append(produced, ex.Schedule(task, nameArg{Producer: producer, Count: 40, Round: round}).Handle())
			}
			return ex.Schedule(commitTask, kernel.NoArg{}, produced...).Handle()
		})
	}
	var left []string
	for _, row := range names.All() {
		left = append(left, *lookup.Get(row.Get().Name))
	}
	alive := 0
	for _, page := range lookup.refs.slots.pages {
		for i := range page {
			if page[i].live {
				alive++
			}
		}
	}
	if alive != len(left) || alive != lookup.refs.liveRefs {
		t.Fatalf("%d refs are alive for %d named entities", alive, len(left))
	}
	slices.Sort(left)
	return left
}

func TestCommandRefsGiveTheSameWorldWithOneWorkerAndMany(t *testing.T) {
	one := runNameProducers(t, 1)
	many := runNameProducers(t, 8)
	if len(one) == 0 || len(one) == 6*40*5 || !slices.Equal(one, many) {
		t.Fatalf("one worker left %d names, eight left %d, or they differ or nothing was despawned", len(one), len(many))
	}
}
